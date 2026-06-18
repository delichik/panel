package dns

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"

	panelerr "panel/internal/platform/errors"
	id "panel/internal/platform/identity"
	"panel/internal/platform/secrets"
)

var domainNamePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)
var recordNamePattern = regexp.MustCompile(`^(@|[a-z0-9_*]([a-z0-9_*.-]{0,251}[a-z0-9_*])?)$`)

type Service struct {
	db              *sql.DB
	secrets         *secretstore.Store
	providerFactory func(domain ResolvedDomain) Provider
}

func NewService(db *sql.DB, secrets *secretstore.Store) *Service {
	return &Service{db: db, secrets: secrets, providerFactory: func(domain ResolvedDomain) Provider {
		return NewCloudflareProvider(domain.APIToken, http.DefaultClient)
	}}
}

func (s *Service) ListDomains(ctx context.Context) ([]Domain, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,provider,created_at,updated_at FROM dns_domains ORDER BY name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Domain{}
	for rows.Next() {
		domain, err := scanDomain(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, domain)
	}
	return out, rows.Err()
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
	_, err = s.db.ExecContext(ctx, `INSERT INTO dns_domains(id,name,provider,provider_config_json,provider_secret_ciphertext,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`,
		domain.ID, domain.Name, domain.Provider, "{}", ciphertext, formatTime(now), formatTime(now))
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
	res, err := s.db.ExecContext(ctx, `UPDATE dns_domains SET name=?,provider=?,provider_config_json=?,provider_secret_ciphertext=?,updated_at=? WHERE id=?`,
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
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM certificates WHERE domain_id=?`, domainID).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return panelerr.Conflict("dns_domain_in_use", "DNS domain is still used by one or more certificates")
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM dns_domains WHERE id=?`, domainID)
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
	domain, provider, err := s.resolveProvider(ctx, domainID)
	if err != nil {
		return nil, err
	}
	return provider.ListRecords(ctx, domain.Name)
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
	return provider.CreateRecord(ctx, domain.Name, record)
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
	return provider.UpdateRecord(ctx, domain.Name, recordID, record)
}

func (s *Service) DeleteRecord(ctx context.Context, domainID, recordID string) error {
	domain, provider, err := s.resolveProvider(ctx, domainID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(recordID) == "" {
		return panelerr.Validation("dns_record_id_required", "DNS record ID is required")
	}
	return provider.DeleteRecord(ctx, domain.Name, recordID)
}

func (s *Service) GetDomain(ctx context.Context, domainID string) (Domain, error) {
	domain, err := scanDomain(s.db.QueryRowContext(ctx, `SELECT id,name,provider,created_at,updated_at FROM dns_domains WHERE id=?`, domainID))
	if err == sql.ErrNoRows {
		return Domain{}, panelerr.NotFound("dns_domain")
	}
	return domain, err
}

func (s *Service) ResolveDomain(ctx context.Context, domainID string) (ResolvedDomain, error) {
	var out ResolvedDomain
	var providerConfigJSON, providerSecretCiphertext string
	var created, updated string
	err := s.db.QueryRowContext(ctx, `SELECT id,name,provider,provider_config_json,provider_secret_ciphertext,created_at,updated_at FROM dns_domains WHERE id=?`, domainID).
		Scan(&out.ID, &out.Name, &out.Provider, &providerConfigJSON, &providerSecretCiphertext, &created, &updated)
	if err == sql.ErrNoRows {
		return ResolvedDomain{}, panelerr.NotFound("dns_domain")
	}
	if err != nil {
		return ResolvedDomain{}, err
	}
	token, err := decryptProviderCredentials(s.secrets, out.ID, out.Provider, providerSecretCiphertext)
	if err != nil {
		return ResolvedDomain{}, err
	}
	out.APIToken = token
	out.CreatedAt = parseTime(created)
	out.UpdatedAt = parseTime(updated)
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

type domainScanner interface{ Scan(dest ...any) error }

func scanDomain(row domainScanner) (Domain, error) {
	var domain Domain
	var created, updated string
	if err := row.Scan(&domain.ID, &domain.Name, &domain.Provider, &created, &updated); err != nil {
		return Domain{}, err
	}
	domain.CreatedAt = parseTime(created)
	domain.UpdatedAt = parseTime(updated)
	return domain, nil
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
