package dns

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"

	"panel/internal/modules/tasks"
	"panel/internal/platform/database/models"
	"panel/internal/platform/database/orm"
	panelerr "panel/internal/platform/errors"
	httpx "panel/internal/platform/http"
	id "panel/internal/platform/identity"
	"panel/internal/platform/secrets"
)

var domainNamePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)
var recordNamePattern = regexp.MustCompile(`^(@|[a-z0-9_*]([a-z0-9_*.-]{0,251}[a-z0-9_*])?)$`)

type Service struct {
	db              *sql.DB
	secrets         *secretstore.Store
	providerFactory func(domain ResolvedDomain) Provider
	tasks           *tasks.Service
}

func NewService(db *sql.DB, secrets *secretstore.Store, taskServices ...*tasks.Service) *Service {
	s := &Service{db: db, secrets: secrets, providerFactory: func(domain ResolvedDomain) Provider {
		return NewCloudflareProvider(domain.APIToken, http.DefaultClient)
	}}
	if len(taskServices) > 0 {
		s.tasks = taskServices[0]
	}
	if s.tasks != nil {
		s.tasks.MustRegister(tasks.Definition{Type: "dns_records_refresh", AllowRetry: true, Execute: s.RunRecordsRefreshTask})
	}
	return s
}

func (s *Service) ListDomains(ctx context.Context) ([]Domain, error) {
	rows := []domainRow{}
	if err := orm.New(s.db).From("dns_domains").Select("id", "name", "provider", "created_at", "updated_at").OrderBy("name").All(ctx, &rows); err != nil {
		return nil, err
	}
	out := make([]Domain, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.domain())
	}
	return out, nil
}

func (s *Service) ListDomainPage(ctx context.Context, page, pageSize int, query string) (httpx.ListPage[Domain], error) {
	countQuery := orm.New(s.db).From("dns_domains")
	itemQuery := orm.New(s.db).From("dns_domains")
	if query != "" {
		term := orm.LikeEscaped(query)
		countQuery.Where("name LIKE ? ESCAPE '\\'", term)
		itemQuery.Where("name LIKE ? ESCAPE '\\'", term)
	}
	total64, err := countQuery.Count(ctx)
	if err != nil {
		return httpx.ListPage[Domain]{}, err
	}
	rows := []domainRow{}
	if err := itemQuery.Select("id", "name", "provider", "created_at", "updated_at").OrderBy("name", "id").Limit(pageSize).Offset((page-1)*pageSize).All(ctx, &rows); err != nil {
		return httpx.ListPage[Domain]{}, err
	}
	items := make([]Domain, 0, len(rows))
	for _, row := range rows {
		items = append(items, row.domain())
	}
	return httpx.ListPage[Domain]{Items: items, Total: int(total64), Page: page, PageSize: pageSize}, nil
}

func (s *Service) CreateDomain(ctx context.Context, in SaveDomainRequest) (Domain, error) {
	prepared, err := validateSaveDomain(in, true)
	if err != nil {
		return Domain{}, err
	}
	if err := s.validateProviderAccess(ctx, resolvedDomainForSave(prepared)); err != nil {
		return Domain{}, err
	}
	now := time.Now().UTC()
	domain := Domain{ID: id.New("dnsdom"), Name: prepared.Name, Provider: prepared.Provider, CreatedAt: now, UpdatedAt: now}
	ciphertext, err := encryptProviderCredentials(s.secrets, domain.ID, domain.Provider, prepared.APIToken)
	if err != nil {
		return Domain{}, err
	}
	err = orm.New(s.db).From("dns_domains").Insert(ctx, &models.DNSDomain{
		ID:                       domain.ID,
		Name:                     domain.Name,
		Provider:                 domain.Provider,
		ProviderConfigJSON:       map[string]any{},
		ProviderSecretCiphertext: ciphertext,
		CreatedAt:                now,
		UpdatedAt:                now,
	})
	if err != nil {
		return Domain{}, err
	}
	return s.GetDomain(ctx, domain.ID)
}

func (s *Service) UpdateDomain(ctx context.Context, domainID string, in SaveDomainRequest) (Domain, error) {
	current, err := s.ResolveDomain(ctx, domainID)
	if err != nil {
		return Domain{}, err
	}
	prepared, err := validateSaveDomain(in, false)
	if err != nil {
		return Domain{}, err
	}
	token := current.APIToken
	if prepared.APIToken != "" {
		token = prepared.APIToken
	}
	if err := s.validateProviderAccess(ctx, ResolvedDomain{
		Domain: Domain{
			ID:        current.ID,
			Name:      prepared.Name,
			Provider:  prepared.Provider,
			CreatedAt: current.CreatedAt,
			UpdatedAt: current.UpdatedAt,
		},
		APIToken: token,
	}); err != nil {
		return Domain{}, err
	}
	now := time.Now().UTC()
	ciphertext, err := encryptProviderCredentials(s.secrets, domainID, prepared.Provider, token)
	if err != nil {
		return Domain{}, err
	}
	res, err := orm.RawExec(ctx, s.db, `UPDATE dns_domains SET name=?,provider=?,provider_config_json=?,provider_secret_ciphertext=?,updated_at=? WHERE id=?`,
		prepared.Name, prepared.Provider, "{}", ciphertext, formatTime(now), domainID)
	if err != nil {
		return Domain{}, err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return Domain{}, panelerr.NotFound("dns_domain")
	}
	return s.GetDomain(ctx, domainID)
}

func (s *Service) DeleteDomain(ctx context.Context, domainID string) error {
	count, err := orm.New(s.db).From("certificates").Where("domain_id = ?", domainID).Count(ctx)
	if err != nil {
		return err
	}
	if count > 0 {
		return panelerr.Conflict("dns_domain_in_use", "DNS domain is still used by one or more certificates")
	}
	res, err := orm.RawExec(ctx, s.db, `DELETE FROM dns_domains WHERE id=?`, domainID)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return panelerr.NotFound("dns_domain")
	}
	return nil
}

func (s *Service) ListRecords(ctx context.Context, domainID string) ([]Record, error) {
	if _, err := s.GetDomain(ctx, domainID); err != nil {
		return nil, err
	}
	var raw string
	err := orm.New(s.db).From("dns_record_snapshots").Select("records_json").Where("domain_id = ?", domainID).ScanValue(ctx, &raw)
	if err == sql.ErrNoRows {
		return []Record{}, nil
	}
	if err != nil {
		return nil, err
	}
	items := []Record{}
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Service) ListRecordSnapshot(ctx context.Context, domainID string) (RecordSnapshot, error) {
	if _, err := s.GetDomain(ctx, domainID); err != nil {
		return RecordSnapshot{}, err
	}
	var raw, observedRaw, lastError string
	err := orm.RawRow(ctx, s.db, `SELECT records_json,observed_at,last_error FROM dns_record_snapshots WHERE domain_id=?`, domainID).Scan(&raw, &observedRaw, &lastError)
	if err == sql.ErrNoRows {
		return RecordSnapshot{Items: []Record{}, Stale: true}, nil
	}
	if err != nil {
		return RecordSnapshot{}, err
	}
	items := []Record{}
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return RecordSnapshot{}, err
	}
	observed, err := time.Parse(time.RFC3339Nano, observedRaw)
	if err != nil {
		return RecordSnapshot{Items: items, Stale: true, LastRefreshError: lastError}, nil
	}
	return RecordSnapshot{Items: items, ObservedAt: &observed, LastRefreshError: lastError}, nil
}

func (s *Service) RefreshRecords(ctx context.Context, domainID string) (tasks.Task, error) {
	if s.tasks == nil {
		return tasks.Task{}, panelerr.Validation("task_service_unavailable", "Task service is unavailable")
	}
	if _, err := s.GetDomain(ctx, domainID); err != nil {
		return tasks.Task{}, err
	}
	task, created, err := tasks.NewManager(s.tasks).Create(ctx, tasks.CreateInput{Type: "dns_records_refresh", ResourceType: "dns_domain", ResourceID: domainID, TriggerType: "user", Summary: "Refreshing DNS records"}, tasks.Trigger{Type: "user"})
	if err != nil || !created {
		return task, err
	}
	if err := s.tasks.Start(ctx, task.ID); err != nil {
		return tasks.Task{}, err
	}
	go func() {
		runCtx := s.tasks.ExecutionContext(task.ID)
		defer s.tasks.FinishExecution(task.ID)
		if err := s.refreshRecords(runCtx, domainID); err != nil {
			_ = s.tasks.Fail(runCtx, task.ID, err)
			return
		}
		_ = s.tasks.Complete(runCtx, task.ID, "DNS records refreshed")
	}()
	return task, nil
}

func (s *Service) RunRecordsRefreshTask(tc tasks.TaskContext) error {
	if err := s.tasks.Start(tc.Context, tc.Task.ID); err != nil {
		return err
	}
	if err := s.refreshRecords(tc.Context, tc.Task.ResourceID); err != nil {
		return err
	}
	return s.tasks.Complete(tc.Context, tc.Task.ID, "DNS records refreshed")
}

func (s *Service) refreshRecords(ctx context.Context, domainID string) error {
	domain, provider, err := s.resolveProvider(ctx, domainID)
	if err != nil {
		return err
	}
	items, err := provider.ListRecords(ctx, domain.Name)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(items)
	if err != nil {
		return err
	}
	_, err = orm.RawExec(ctx, s.db, `INSERT INTO dns_record_snapshots(domain_id,records_json,observed_at,last_error) VALUES(?,?,?,'') ON CONFLICT(domain_id) DO UPDATE SET records_json=excluded.records_json,observed_at=excluded.observed_at,last_error=''`, domainID, string(raw), formatTime(time.Now().UTC()))
	return err
}

func (s *Service) CreateRecord(ctx context.Context, domainID string, in RecordInput) (Record, error) {
	domain, provider, err := s.resolveProvider(ctx, domainID)
	if err != nil {
		return Record{}, err
	}
	record, err := validateRecordInput(in)
	if err != nil {
		return Record{}, err
	}
	created, err := provider.CreateRecord(ctx, domain.Name, record)
	if err == nil {
		_ = s.refreshRecords(ctx, domainID)
	}
	return created, err
}

func (s *Service) UpdateRecord(ctx context.Context, domainID, recordID string, in RecordInput) (Record, error) {
	domain, provider, err := s.resolveProvider(ctx, domainID)
	if err != nil {
		return Record{}, err
	}
	if strings.TrimSpace(recordID) == "" {
		return Record{}, panelerr.Validation("dns_record_id_required", "DNS record ID is required")
	}
	record, err := validateRecordInput(in)
	if err != nil {
		return Record{}, err
	}
	updated, err := provider.UpdateRecord(ctx, domain.Name, recordID, record)
	if err == nil {
		_ = s.refreshRecords(ctx, domainID)
	}
	return updated, err
}

func (s *Service) DeleteRecord(ctx context.Context, domainID, recordID string) error {
	domain, provider, err := s.resolveProvider(ctx, domainID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(recordID) == "" {
		return panelerr.Validation("dns_record_id_required", "DNS record ID is required")
	}
	err = provider.DeleteRecord(ctx, domain.Name, recordID)
	if err == nil {
		_ = s.refreshRecords(ctx, domainID)
	}
	return err
}

func (s *Service) GetDomain(ctx context.Context, domainID string) (Domain, error) {
	var row domainRow
	err := orm.New(s.db).From("dns_domains").Select("id", "name", "provider", "created_at", "updated_at").Where("id = ?", domainID).First(ctx, &row)
	if err == sql.ErrNoRows {
		return Domain{}, panelerr.NotFound("dns_domain")
	}
	if err != nil {
		return Domain{}, err
	}
	return row.domain(), nil
}

func (s *Service) ResolveDomain(ctx context.Context, domainID string) (ResolvedDomain, error) {
	var row domainRow
	err := orm.New(s.db).From("dns_domains").Select("id", "name", "provider", "provider_config_json", "provider_secret_ciphertext", "created_at", "updated_at").Where("id = ?", domainID).First(ctx, &row)
	if err == sql.ErrNoRows {
		return ResolvedDomain{}, panelerr.NotFound("dns_domain")
	}
	if err != nil {
		return ResolvedDomain{}, err
	}
	token, err := decryptProviderCredentials(s.secrets, row.ID, row.Provider, row.ProviderSecretCiphertext)
	if err != nil {
		return ResolvedDomain{}, err
	}
	out := ResolvedDomain{Domain: row.domain(), APIToken: token}
	return out, nil
}

type preparedDomain struct {
	Name     string
	Provider string
	APIToken string
}

func validateSaveDomain(in SaveDomainRequest, requireToken bool) (preparedDomain, error) {
	name := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(in.Name), "."))
	if !domainNamePattern.MatchString(name) {
		return preparedDomain{}, panelerr.Validation("dns_domain_invalid", "Domain must be a valid DNS name")
	}
	provider := strings.TrimSpace(in.Provider)
	if provider == "" {
		provider = ProviderCloudflare
	}
	if provider != ProviderCloudflare {
		return preparedDomain{}, panelerr.Validation("dns_provider_invalid", "DNS provider must be cloudflare")
	}
	token := strings.TrimSpace(in.APIToken)
	if requireToken && token == "" {
		return preparedDomain{}, panelerr.Validation("dns_api_token_required", "DNS provider API token is required")
	}
	return preparedDomain{Name: name, Provider: provider, APIToken: token}, nil
}

func resolvedDomainForSave(prepared preparedDomain) ResolvedDomain {
	return ResolvedDomain{
		Domain: Domain{
			Name:     prepared.Name,
			Provider: prepared.Provider,
		},
		APIToken: prepared.APIToken,
	}
}

func (s *Service) validateProviderAccess(ctx context.Context, domain ResolvedDomain) error {
	if domain.Provider != ProviderCloudflare {
		return panelerr.Validation("dns_provider_invalid", "DNS provider must be cloudflare")
	}
	_, err := s.providerFactory(domain).ListRecords(ctx, domain.Name)
	return err
}

func (s *Service) resolveProvider(ctx context.Context, domainID string) (ResolvedDomain, Provider, error) {
	domain, err := s.ResolveDomain(ctx, domainID)
	if err != nil {
		return ResolvedDomain{}, nil, err
	}
	if domain.Provider != ProviderCloudflare {
		return ResolvedDomain{}, nil, panelerr.Validation("dns_provider_invalid", "DNS provider must be cloudflare")
	}
	return domain, s.providerFactory(domain), nil
}

func validateRecordInput(in RecordInput) (RecordInput, error) {
	name := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(in.Name), "."))
	if !recordNamePattern.MatchString(name) {
		return RecordInput{}, panelerr.Validation("dns_record_name_invalid", "DNS record name is invalid")
	}
	recordType := strings.ToUpper(strings.TrimSpace(in.Type))
	if !supportedRecordType(recordType) {
		return RecordInput{}, panelerr.Validation("dns_record_type_invalid", "DNS record type is invalid")
	}
	value := strings.TrimSpace(in.Value)
	if value == "" {
		return RecordInput{}, panelerr.Validation("dns_record_value_required", "DNS record value is required")
	}
	if in.TTL < 0 {
		return RecordInput{}, panelerr.Validation("dns_record_ttl_invalid", "DNS record TTL cannot be negative")
	}
	proxied := in.Proxied
	if !supportsProxy(recordType) {
		if proxied != nil && *proxied {
			return RecordInput{}, panelerr.Validation("dns_record_proxy_invalid", "DNS record proxy is only supported for A, AAAA, and CNAME records")
		}
		proxied = nil
	}
	return RecordInput{Name: name, Type: recordType, Value: value, TTL: in.TTL, Proxied: proxied}, nil
}

func supportsProxy(value string) bool {
	switch value {
	case "A", "AAAA", "CNAME":
		return true
	default:
		return false
	}
}

func supportedRecordType(value string) bool {
	switch value {
	case "A", "AAAA", "CNAME", "TXT", "MX", "SRV", "CAA", "NS":
		return true
	default:
		return false
	}
}

// domainRow 是 dns_domains 的本地行映射：created_at/updated_at 在遗留库中可能
// 不是 RFC3339Nano（如迁移测试中的 'now'），必须按字符串透传并容错解析。
type domainRow struct {
	ID                       string
	Name                     string
	Provider                 string
	ProviderConfigJSON       string
	ProviderSecretCiphertext string
	CreatedAt                string
	UpdatedAt                string
}

func (row domainRow) domain() Domain {
	return Domain{
		ID:        row.ID,
		Name:      row.Name,
		Provider:  row.Provider,
		CreatedAt: parseTime(row.CreatedAt),
		UpdatedAt: parseTime(row.UpdatedAt),
	}
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, value)
	return parsed
}

type cloudflareCredentials struct {
	APIToken string `json:"apiToken"`
}

const providerCredentialsField = "provider_credentials"

func encryptProviderCredentials(secrets *secretstore.Store, domainID, provider, apiToken string) (string, error) {
	if secrets == nil {
		return "", panelerr.Validation("dns_provider_credentials_unavailable", "DNS provider credential store is unavailable")
	}
	var credentials any
	switch provider {
	case ProviderCloudflare:
		if strings.TrimSpace(apiToken) == "" {
			return "", panelerr.Validation("cloudflare_api_token_required", "Cloudflare API token is required")
		}
		credentials = cloudflareCredentials{APIToken: apiToken}
	default:
		return "", panelerr.Validation("dns_provider_invalid", "Unsupported DNS provider")
	}
	raw, err := json.Marshal(credentials)
	if err != nil {
		return "", err
	}
	return secrets.Encrypt(domainID, provider, providerCredentialsField, raw)
}

func decryptProviderCredentials(secrets *secretstore.Store, domainID, provider, ciphertext string) (string, error) {
	if secrets == nil {
		return "", panelerr.Validation("dns_provider_credentials_unavailable", "DNS provider credential store is unavailable")
	}
	raw, err := secrets.Decrypt(domainID, provider, providerCredentialsField, ciphertext)
	if err != nil {
		return "", err
	}
	switch provider {
	case ProviderCloudflare:
		var credentials cloudflareCredentials
		if err := json.Unmarshal(raw, &credentials); err != nil {
			return "", panelerr.Validation("dns_provider_credentials_invalid", "DNS provider credentials are invalid")
		}
		if strings.TrimSpace(credentials.APIToken) == "" {
			return "", panelerr.Validation("cloudflare_api_token_required", "Cloudflare API token is required")
		}
		return credentials.APIToken, nil
	default:
		return "", panelerr.Validation("dns_provider_invalid", "Unsupported DNS provider")
	}
}
