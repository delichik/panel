package certs

import (
	"context"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"panel/internal/modules/applications"
	"panel/internal/modules/certificates/dns"
	"panel/internal/modules/certificates/proxycert"
	"panel/internal/modules/keyassets"
	"panel/internal/modules/tasks"
	"panel/internal/platform/config"
	panelerr "panel/internal/platform/errors"
	id "panel/internal/platform/identity"
)

var (
	domainPattern       = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)
	variableNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

type Service struct {
	db               *sql.DB
	dataRoot         string
	cfg              config.Config
	configProvider   func(config.Config) config.Config
	domains          domainResolver
	providerOverride Provider
	tasks            *tasks.Service
	issuer           string
	applications     applicationRefresher
	keyAssets        keyAssetProvider
}

type domainResolver interface {
	ResolveDomain(ctx context.Context, domainID string) (dns.ResolvedDomain, error)
}

type applicationRefresher interface {
	RedeployChangedApplications(ctx context.Context) (int, error)
	RedeployEnabledApplications(ctx context.Context) (int, error)
	ReconcileReverseProxy(ctx context.Context) error
}

type keyAssetProvider interface {
	List(ctx context.Context) ([]keyassets.Asset, error)
	CreateCA(ctx context.Context, in keyassets.CreateCARequest) (keyassets.Asset, error)
	CreateTLS(ctx context.Context, in keyassets.CreateTLSRequest) (keyassets.Asset, error)
	ReissueTLS(ctx context.Context, assetID string) (keyassets.ReissueResult, error)
	Delete(ctx context.Context, assetID string) error
	PanelFileCatalog(ctx context.Context) ([]applications.PanelFileDefinition, error)
	ReadPanelFile(ctx context.Context, source string) ([]byte, error)
	ReverseProxyCertificates(ctx context.Context) ([]proxycert.Certificate, error)
}

type Option func(*Service)

func WithApplicationRefresher(refresher applicationRefresher) Option {
	return func(s *Service) { s.applications = refresher }
}

func WithKeyAssetProvider(provider keyAssetProvider) Option {
	return func(s *Service) { s.keyAssets = provider }
}

func WithConfigProvider(provider func(config.Config) config.Config) Option {
	return func(s *Service) { s.configProvider = provider }
}

func NewService(db *sql.DB, cfg config.Config, domains domainResolver, taskSvc *tasks.Service, opts ...Option) *Service {
	s := &Service{db: db, dataRoot: cfg.DataRoot, cfg: cfg, domains: domains, tasks: taskSvc, issuer: "acme"}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func NewServiceWithProvider(db *sql.DB, cfg config.Config, provider Provider, taskSvc *tasks.Service, opts ...Option) *Service {
	s := &Service{db: db, dataRoot: cfg.DataRoot, cfg: cfg, providerOverride: provider, tasks: taskSvc, issuer: "acme"}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Service) currentConfig() config.Config {
	cfg := s.cfg
	if s.configProvider != nil {
		cfg = s.configProvider(cfg)
	}
	return cfg
}

func (s *Service) Issue(ctx context.Context, in IssueRequest) (IssueResult, error) {
	return s.QueueIssue(ctx, in)
}

func (s *Service) QueueIssue(ctx context.Context, in IssueRequest) (IssueResult, error) {
	resolved, err := s.resolveDomain(ctx, in.DomainID)
	if err != nil {
		return IssueResult{}, err
	}
	prepared, err := prepareIssueRequest(in, resolved.Name)
	if err != nil {
		return IssueResult{}, err
	}

	now := time.Now().UTC()
	cert := Certificate{
		ID:           id.New("cert"),
		Name:         prepared.Name,
		DomainID:     resolved.ID,
		Domain:       prepared.Domain,
		Prefix:       prepared.Prefix,
		Scope:        prepared.Scope,
		Domains:      prepared.Domains,
		VariableName: prepared.VariableName,
		Issuer:       s.issuer,
		Status:       StatusPending,
		AutoRenew:    true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	certDir := filepath.Join(s.dataRoot, "certs", cert.ID)
	if err := os.MkdirAll(certDir, 0700); err != nil {
		return IssueResult{}, err
	}
	cert.CertificatePath = filepath.Join(certDir, "certificate.pem")
	cert.PrivateKeyPath = filepath.Join(certDir, "private-key.pem")
	if err := s.insert(ctx, cert); err != nil {
		return IssueResult{}, err
	}
	taskID, err := s.recordTask(ctx, TaskTypeIssue, cert, tasks.StatusQueued, "Issue certificate for "+cert.Domain)
	if err != nil {
		return IssueResult{}, err
	}
	cert, err = s.Get(ctx, cert.ID)
	if err != nil {
		return IssueResult{}, err
	}
	return IssueResult{Certificate: cert, TaskID: taskID}, nil
}

func (s *Service) RunIssueTask(tc tasks.TaskContext) error {
	ctx, task := tc.Context, tc.Task
	if s.tasks == nil {
		return nil
	}
	if err := s.tasks.Start(ctx, task.ID); err != nil {
		return err
	}
	defer s.tasks.FinishExecution(task.ID)
	if err := s.tasks.Advance(ctx, task.ID, "preparing", "Preparing certificate request"); err != nil {
		return err
	}
	cert, err := s.Get(ctx, task.ResourceID)
	if err != nil {
		_ = s.tasks.Fail(ctx, task.ID, err)
		return err
	}
	if cert.Status == StatusIssued {
		return s.tasks.Complete(ctx, task.ID, "Certificate already issued for "+cert.Domain)
	}
	if err := s.updateStatus(ctx, cert.ID, StatusIssuing, ""); err != nil {
		_ = s.tasks.Fail(ctx, task.ID, err)
		return err
	}
	if err := s.tasks.Advance(ctx, task.ID, "running", "Running ACME DNS-01 challenge"); err != nil {
		return err
	}
	if err := s.issueIntoCertificate(ctx, cert, task.ID); err != nil {
		_ = s.updateStatus(ctx, cert.ID, StatusFailed, err.Error())
		_ = s.tasks.Fail(ctx, task.ID, err)
		return err
	}
	if err := s.updateStatus(ctx, cert.ID, StatusIssued, ""); err != nil {
		_ = s.tasks.Fail(ctx, task.ID, err)
		return err
	}
	if err := s.refreshApplications(ctx); err != nil {
		_ = s.tasks.Fail(ctx, task.ID, err)
		return err
	}
	return s.tasks.Complete(ctx, task.ID, "Issued certificate for "+cert.Domain)
}

func (s *Service) issueIntoCertificate(ctx context.Context, cert Certificate, taskID string) error {
	resolved, err := s.resolveDomain(ctx, cert.DomainID)
	if err != nil {
		return err
	}
	provider, err := s.providerForDomain(resolved)
	if err != nil {
		return err
	}
	bundle, err := provider.Issue(ctx, Request{Domain: cert.Domain, Domains: cert.Domains, Progress: s.acmeProgress(taskID)})
	if err != nil {
		return err
	}
	if err := validateBundle(bundle); err != nil {
		return err
	}
	if err := os.WriteFile(cert.CertificatePath, append(bundle.CertificatePEM, bundle.CAChainPEM...), 0600); err != nil {
		return err
	}
	if err := os.WriteFile(cert.PrivateKeyPath, bundle.PrivateKeyPEM, 0600); err != nil {
		return err
	}
	cert.NotBefore, cert.NotAfter = certificateValidity(bundle.CertificatePEM)
	cert.NextRenewAt = nextRenewAt(cert.NotAfter)
	cert.UpdatedAt = time.Now().UTC()
	return s.updateRenewal(ctx, cert)
}

func (s *Service) RenewTask(tc tasks.TaskContext) error {
	return s.runRenewTask(tc.Context, tc.Task)
}

func (s *Service) Renew(ctx context.Context, certID string) error {
	cert, err := s.Get(ctx, certID)
	if err != nil {
		return err
	}
	taskID, err := s.recordTask(ctx, TaskTypeRenew, cert, tasks.StatusRunning, "Renewing certificate for "+cert.Domain)
	if err != nil {
		return err
	}
	return s.runRenewTask(ctx, tasks.Task{ID: taskID, Type: TaskTypeRenew, ResourceType: "certificate", ResourceID: cert.ID})
}

func (s *Service) runRenewTask(ctx context.Context, task tasks.Task) error {
	certID := firstNonEmpty(task.ResourceID, task.TriggerResourceID)
	if strings.TrimSpace(certID) == "" {
		return panelerr.Validation("certificate_required", "Certificate is required")
	}
	cert, err := s.Get(ctx, certID)
	if err != nil {
		if task.ID != "" {
			_ = s.failRenewalTask(ctx, task.ID, err)
		}
		return err
	}
	if task.ID != "" {
		if err := s.tasks.Start(ctx, task.ID); err != nil {
			return err
		}
		defer s.tasks.FinishExecution(task.ID)
		_ = s.tasks.Advance(ctx, task.ID, "running", "Running ACME DNS-01 renewal")
	}
	resolved, err := s.resolveDomain(ctx, cert.DomainID)
	if err != nil {
		_ = s.updateLastError(ctx, cert.ID, err.Error())
		_ = s.failRenewalTask(ctx, task.ID, err)
		return err
	}
	provider, err := s.providerForDomain(resolved)
	if err != nil {
		_ = s.updateLastError(ctx, cert.ID, err.Error())
		_ = s.failRenewalTask(ctx, task.ID, err)
		return err
	}
	bundle, err := provider.Issue(ctx, Request{Domain: cert.Domain, Domains: cert.Domains, Progress: s.acmeProgress(task.ID)})
	if err != nil {
		_ = s.updateLastError(ctx, cert.ID, err.Error())
		_ = s.failRenewalTask(ctx, task.ID, err)
		return err
	}
	if err := validateBundle(bundle); err != nil {
		_ = s.updateLastError(ctx, cert.ID, err.Error())
		_ = s.failRenewalTask(ctx, task.ID, err)
		return err
	}
	if err := replaceCertificateFiles(
		cert.CertificatePath,
		append(bundle.CertificatePEM, bundle.CAChainPEM...),
		cert.PrivateKeyPath,
		bundle.PrivateKeyPEM,
	); err != nil {
		_ = s.updateLastError(ctx, cert.ID, err.Error())
		_ = s.failRenewalTask(ctx, task.ID, err)
		return err
	}
	cert.NotBefore, cert.NotAfter = certificateValidity(bundle.CertificatePEM)
	cert.NextRenewAt = nextRenewAt(cert.NotAfter)
	cert.LastError = ""
	cert.UpdatedAt = time.Now().UTC()
	if err := s.updateRenewal(ctx, cert); err != nil {
		_ = s.updateLastError(ctx, cert.ID, err.Error())
		_ = s.failRenewalTask(ctx, task.ID, err)
		return err
	}
	if err := s.refreshApplications(ctx); err != nil {
		_ = s.updateLastError(ctx, cert.ID, err.Error())
		_ = s.failRenewalTask(ctx, task.ID, err)
		return err
	}
	if task.ID != "" {
		return s.tasks.Complete(ctx, task.ID, "Renewed certificate for "+cert.Domain)
	}
	return nil
}

func replaceCertificateFiles(certificatePath string, certificatePEM []byte, privateKeyPath string, privateKeyPEM []byte) error {
	certTemp, err := writeCertificateTemp(certificatePath, certificatePEM)
	if err != nil {
		return err
	}
	defer os.Remove(certTemp)
	keyTemp, err := writeCertificateTemp(privateKeyPath, privateKeyPEM)
	if err != nil {
		return err
	}
	defer os.Remove(keyTemp)

	certBackup := certificatePath + ".previous"
	keyBackup := privateKeyPath + ".previous"
	_ = os.Remove(certBackup)
	_ = os.Remove(keyBackup)
	if err := os.Rename(certificatePath, certBackup); err != nil && !os.IsNotExist(err) {
		return err
	}
	certBackedUp := true
	defer func() {
		if certBackedUp {
			_ = os.Rename(certBackup, certificatePath)
		}
	}()
	if err := os.Rename(privateKeyPath, keyBackup); err != nil && !os.IsNotExist(err) {
		return err
	}
	keyBackedUp := true
	defer func() {
		if keyBackedUp {
			_ = os.Rename(keyBackup, privateKeyPath)
		}
	}()
	if err := os.Rename(certTemp, certificatePath); err != nil {
		return err
	}
	if err := os.Rename(keyTemp, privateKeyPath); err != nil {
		_ = os.Remove(certificatePath)
		return err
	}
	certBackedUp = false
	keyBackedUp = false
	_ = os.Remove(certBackup)
	_ = os.Remove(keyBackup)
	return nil
}

func writeCertificateTemp(target string, content []byte) (string, error) {
	file, err := os.CreateTemp(filepath.Dir(target), filepath.Base(target)+".tmp-*")
	if err != nil {
		return "", err
	}
	tempPath := file.Name()
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		_ = os.Remove(tempPath)
		return "", err
	}
	if _, err := file.Write(content); err != nil {
		file.Close()
		_ = os.Remove(tempPath)
		return "", err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		_ = os.Remove(tempPath)
		return "", err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tempPath)
		return "", err
	}
	return tempPath, nil
}

func (s *Service) List(ctx context.Context) ([]Certificate, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+certificateColumns+` FROM certificates ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Certificate{}
	for rows.Next() {
		cert, err := scanCertificate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, cert)
	}
	return out, rows.Err()
}

func (s *Service) Get(ctx context.Context, certID string) (Certificate, error) {
	cert, err := scanCertificate(s.db.QueryRowContext(ctx, `SELECT `+certificateColumns+` FROM certificates WHERE id=?`, certID))
	if err == sql.ErrNoRows {
		return Certificate{}, panelerr.NotFound("certificate")
	}
	return cert, err
}

func (s *Service) Delete(ctx context.Context, certID string) error {
	cert, err := s.Get(ctx, certID)
	if err != nil {
		return err
	}
	if used, err := s.certificateInUse(ctx, cert.ID, cert.Domains, cert.VariableName); err != nil {
		return err
	} else if used {
		return panelerr.Conflict("certificate_in_use", "Certificate is still used by an application or reverse proxy")
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM certificates WHERE id=?`, certID)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return panelerr.NotFound("certificate")
	}
	if cert.CertificatePath != "" {
		_ = os.Remove(cert.CertificatePath)
	}
	if cert.PrivateKeyPath != "" {
		_ = os.Remove(cert.PrivateKeyPath)
	}
	_ = os.Remove(filepath.Dir(cert.CertificatePath))
	return nil
}

func (s *Service) BuiltinVariables(ctx context.Context) (map[string]any, error) {
	certs, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	certVars := map[string]any{}
	for _, cert := range certs {
		if cert.Status != StatusIssued {
			continue
		}
		certPEM, err := os.ReadFile(cert.CertificatePath)
		if err != nil {
			return nil, err
		}
		keyPEM, err := os.ReadFile(cert.PrivateKeyPath)
		if err != nil {
			return nil, err
		}
		certVars[cert.VariableName] = map[string]any{
			"certificatePem":  string(certPEM),
			"privateKeyPem":   string(keyPEM),
			"certificate_pem": string(certPEM),
			"private_key_pem": string(keyPEM),
			"domains":         append([]string(nil), cert.Domains...),
		}
	}
	return map[string]any{"certs": certVars}, nil
}

func (s *Service) PanelFileCatalog(ctx context.Context) ([]applications.PanelFileDefinition, error) {
	out := []applications.PanelFileDefinition{}
	certs, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, cert := range certs {
		if cert.Status != StatusIssued {
			continue
		}
		out = append(out,
			applications.PanelFileDefinition{ID: cert.ID + ":certificate", ResourceID: cert.ID, ResourceType: "acme", Name: cert.Name, Kind: "certificate", Source: "certificate:" + cert.ID + ":certificate"},
			applications.PanelFileDefinition{ID: cert.ID + ":private_key", ResourceID: cert.ID, ResourceType: "acme", Name: cert.Name, Kind: "private_key", Source: "certificate:" + cert.ID + ":private_key"},
		)
	}
	if s.keyAssets != nil {
		files, err := s.keyAssets.PanelFileCatalog(ctx)
		if err != nil {
			return nil, err
		}
		out = append(out, files...)
	} else {
		selfSigned, err := s.ListSelfSigned(ctx)
		if err != nil {
			return nil, err
		}
		for _, cert := range selfSigned {
			out = append(out,
				applications.PanelFileDefinition{ID: cert.ID + ":certificate", ResourceID: cert.ID, ResourceType: "self_signed", Name: cert.Name, Kind: "certificate", Source: "certificate:" + cert.ID + ":certificate"},
				applications.PanelFileDefinition{ID: cert.ID + ":private_key", ResourceID: cert.ID, ResourceType: "self_signed", Name: cert.Name, Kind: "private_key", Source: "certificate:" + cert.ID + ":private_key"},
				applications.PanelFileDefinition{ID: cert.ID + ":public_key", ResourceID: cert.ID, ResourceType: "self_signed", Name: cert.Name, Kind: "public_key", Source: "certificate:" + cert.ID + ":public_key"},
			)
		}
	}
	return out, nil
}

func (s *Service) ReadPanelFile(ctx context.Context, source string) ([]byte, error) {
	parts := strings.Split(strings.TrimSpace(source), ":")
	if len(parts) != 3 || (parts[0] != "certificate" && parts[0] != "key_asset") {
		return nil, panelerr.Validation("panel_file_source_invalid", "Panel file source is invalid")
	}
	if parts[0] == "key_asset" {
		if s.keyAssets == nil {
			return nil, panelerr.NotFound("Panel key asset file")
		}
		return s.keyAssets.ReadPanelFile(ctx, source)
	}
	if cert, err := s.Get(ctx, parts[1]); err == nil {
		switch parts[2] {
		case "certificate":
			return os.ReadFile(cert.CertificatePath)
		case "private_key":
			return os.ReadFile(cert.PrivateKeyPath)
		}
	}
	if s.keyAssets != nil {
		if data, err := s.keyAssets.ReadPanelFile(ctx, source); err == nil {
			return data, nil
		}
	}
	cert, err := s.GetSelfSigned(ctx, parts[1])
	if err != nil {
		return nil, panelerr.NotFound("Panel certificate file")
	}
	switch parts[2] {
	case "certificate":
	case "private_key":
	case "public_key":
		if s.keyAssets != nil {
			return s.keyAssets.ReadPanelFile(ctx, "key_asset:"+cert.ID+":"+parts[2])
		}
	default:
		return nil, panelerr.Validation("panel_file_kind_invalid", "Panel certificate file kind is invalid")
	}
	return nil, panelerr.NotFound("Panel certificate file")
}

func (s *Service) certificateInUse(ctx context.Context, certID string, domains []string, variableName string) (bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT spec_yaml,reverse_proxy_json FROM applications`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	sourcePrefix := "certificate:" + certID + ":"
	for rows.Next() {
		var spec, proxy string
		if err := rows.Scan(&spec, &proxy); err != nil {
			return false, err
		}
		if strings.Contains(spec, sourcePrefix) {
			return true, nil
		}
		if variableName != "" && strings.Contains(spec, ".certs."+variableName) {
			return true, nil
		}
		var rules []struct {
			Domain string `json:"domain"`
		}
		_ = json.Unmarshal([]byte(proxy), &rules)
		for _, rule := range rules {
			for _, domain := range domains {
				if certificateDomainMatches(domain, rule.Domain) {
					return true, nil
				}
			}
		}
	}
	return false, rows.Err()
}

func certificateDomainMatches(pattern, domain string) bool {
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	domain = strings.ToLower(strings.TrimSpace(domain))
	if pattern == "" || domain == "" {
		return false
	}
	if pattern == domain {
		return true
	}
	if strings.HasPrefix(pattern, "*.") {
		suffix := strings.TrimPrefix(pattern, "*")
		if strings.HasSuffix(domain, suffix) && domain != strings.TrimPrefix(suffix, ".") {
			return true
		}
	}
	return false
}

func (s *Service) ReverseProxyCertificates(ctx context.Context) ([]proxycert.Certificate, error) {
	certs, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	out := []proxycert.Certificate{}
	for _, cert := range certs {
		if cert.Status != StatusIssued {
			continue
		}
		certPEM, err := os.ReadFile(cert.CertificatePath)
		if err != nil {
			return nil, err
		}
		keyPEM, err := os.ReadFile(cert.PrivateKeyPath)
		if err != nil {
			return nil, err
		}
		out = append(out, proxycert.Certificate{
			ID:             cert.ID,
			Domains:        append([]string(nil), cert.Domains...),
			CertificatePEM: string(certPEM),
			PrivateKeyPEM:  string(keyPEM),
		})
	}
	if s.keyAssets != nil {
		managed, err := s.keyAssets.ReverseProxyCertificates(ctx)
		if err != nil {
			return nil, err
		}
		out = append(out, managed...)
	}
	return out, nil
}

func (s *Service) insert(ctx context.Context, cert Certificate) error {
	domains, err := json.Marshal(cert.Domains)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO certificates(id,name,domain_id,domain,prefix,scope,domains_json,variable_name,certificate_path,private_key_path,issuer,status,last_error,auto_renew,next_renew_at,not_before,not_after,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		cert.ID, cert.Name, cert.DomainID, cert.Domain, cert.Prefix, cert.Scope, string(domains), cert.VariableName, cert.CertificatePath, cert.PrivateKeyPath, cert.Issuer, cert.Status, cert.LastError, boolInt(cert.AutoRenew), formatOptionalTime(cert.NextRenewAt), formatOptionalTime(cert.NotBefore), formatOptionalTime(cert.NotAfter), formatTime(cert.CreatedAt), formatTime(cert.UpdatedAt))
	return err
}

func (s *Service) updateRenewal(ctx context.Context, cert Certificate) error {
	_, err := s.db.ExecContext(ctx, `UPDATE certificates SET not_before=?,not_after=?,next_renew_at=?,last_error=?,updated_at=? WHERE id=?`,
		formatOptionalTime(cert.NotBefore), formatOptionalTime(cert.NotAfter), formatOptionalTime(cert.NextRenewAt), cert.LastError, formatTime(cert.UpdatedAt), cert.ID)
	return err
}

func (s *Service) updateStatus(ctx context.Context, certID, status, lastError string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE certificates SET status=?,last_error=?,updated_at=? WHERE id=?`,
		status, lastError, formatTime(time.Now().UTC()), certID)
	return err
}

func (s *Service) updateLastError(ctx context.Context, certID, lastError string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE certificates SET last_error=?,updated_at=? WHERE id=?`,
		lastError, formatTime(time.Now().UTC()), certID)
	return err
}

func (s *Service) failRenewalTask(ctx context.Context, taskID string, err error) error {
	if s.tasks == nil || taskID == "" {
		return nil
	}
	return s.tasks.Fail(ctx, taskID, err)
}

func (s *Service) acmeProgress(taskID string) func(context.Context, ACMEProgress) {
	if s.tasks == nil || taskID == "" {
		return nil
	}
	return func(ctx context.Context, event ACMEProgress) {
		if event.Stage == "" {
			event.Stage = "running"
		}
		message := event.Message
		if event.Domain != "" {
			message = event.Domain + ": " + message
		}
		_ = s.tasks.Advance(ctx, taskID, event.Stage, message)
		_, _ = s.tasks.UpsertStep(ctx, taskID, tasks.StepInput{
			Step:         event.Stage,
			Status:       tasks.StatusRunning,
			Percentage:   acmeStagePercentage(event.Stage),
			MetadataJSON: acmeProgressMetadata(event),
		})
	}
}

func acmeStagePercentage(stage string) float64 {
	switch stage {
	case "acme_account":
		return 15
	case "acme_order":
		return 30
	case "acme_authorization":
		return 55
	case "acme_dns_challenge":
		return 65
	case "acme_dns_cleanup":
		return 75
	case "acme_finalize":
		return 90
	default:
		return 50
	}
}

func acmeProgressMetadata(event ACMEProgress) string {
	value := map[string]any{
		"stage":   event.Stage,
		"domain":  event.Domain,
		"message": event.Message,
	}
	for key, item := range event.Metadata {
		value[key] = item
	}
	data, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func (s *Service) recordTask(ctx context.Context, taskType string, cert Certificate, status string, summary string) (string, error) {
	if s.tasks == nil {
		return "", nil
	}
	task, _, err := tasks.NewManager(s.tasks).Create(ctx, tasks.CreateInput{
		Type:         taskType,
		ResourceType: "certificate",
		ResourceID:   cert.ID,
		Status:       status,
		MetadataJSON: certTaskMetadataJSON(cert),
		Summary:      summary,
	}, tasks.Trigger{Type: "system"})
	if err != nil {
		return "", err
	}
	return task.ID, nil
}

func certTaskMetadataJSON(cert Certificate) string {
	data, err := json.Marshal(map[string]any{
		"certificateId": cert.ID,
		"domain":        cert.Domain,
		"domains":       cert.Domains,
		"issuer":        cert.Issuer,
	})
	if err != nil {
		return "{}"
	}
	return string(data)
}

func (s *Service) refreshApplications(ctx context.Context) error {
	if s.applications == nil {
		return nil
	}
	if _, err := s.applications.RedeployChangedApplications(ctx); err != nil {
		return err
	}
	return s.applications.ReconcileReverseProxy(ctx)
}

type preparedIssueRequest struct {
	Name         string
	Domain       string
	Prefix       string
	Scope        string
	Domains      []string
	VariableName string
}

func prepareIssueRequest(in IssueRequest, managedDomain string) (preparedIssueRequest, error) {
	prefix := normalizePrefix(in.Prefix)
	domain := joinDomain(prefix, managedDomain)
	if !domainPattern.MatchString(domain) {
		return preparedIssueRequest{}, panelerr.Validation("certificate_domain_invalid", "Domain must be a valid DNS name")
	}
	scope := strings.TrimSpace(in.Scope)
	if scope == "" {
		scope = ScopeSingle
	}
	if scope != ScopeSingle && scope != ScopeWildcard {
		return preparedIssueRequest{}, panelerr.Validation("certificate_scope_invalid", "Certificate scope must be single or wildcard")
	}
	domains := []string{domain}
	if scope == ScopeWildcard {
		domains = []string{domain, "*." + domain}
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = domain
	}
	variableName := strings.TrimSpace(in.VariableName)
	if variableName == "" {
		variableName = defaultVariableName(domain)
	}
	if !variableNamePattern.MatchString(variableName) {
		return preparedIssueRequest{}, panelerr.Validation("certificate_variable_invalid", "Variable name must start with a letter or underscore and contain only letters, digits, or underscores")
	}
	return preparedIssueRequest{Name: name, Domain: domain, Prefix: prefix, Scope: scope, Domains: domains, VariableName: variableName}, nil
}

func normalizePrefix(prefix string) string {
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	prefix = strings.TrimPrefix(prefix, "*.")
	prefix = strings.TrimSuffix(prefix, ".")
	if prefix == "" {
		return "@"
	}
	return prefix
}

func joinDomain(prefix, managedDomain string) string {
	managedDomain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(managedDomain), "."))
	if prefix == "@" {
		return managedDomain
	}
	return prefix + "." + managedDomain
}

func defaultVariableName(domain string) string {
	replacer := strings.NewReplacer(".", "_", "-", "_")
	return replacer.Replace(domain)
}

func validateBundle(bundle Bundle) error {
	if len(bundle.CertificatePEM) == 0 || len(bundle.PrivateKeyPEM) == 0 {
		return panelerr.BadGateway("certificate_issue_failed", "Certificate provider returned an incomplete certificate bundle")
	}
	if block, _ := pem.Decode(bundle.CertificatePEM); block == nil || block.Type != "CERTIFICATE" {
		return panelerr.BadGateway("certificate_issue_failed", "Certificate provider returned invalid certificate PEM")
	}
	if block, _ := pem.Decode(bundle.PrivateKeyPEM); block == nil {
		return panelerr.BadGateway("certificate_issue_failed", "Certificate provider returned invalid private key PEM")
	}
	return nil
}

func certificateValidity(certPEM []byte) (time.Time, time.Time) {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return time.Time{}, time.Time{}
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Time{}, time.Time{}
	}
	return cert.NotBefore, cert.NotAfter
}

type certScanner interface{ Scan(dest ...any) error }

const certificateColumns = `id,name,domain_id,domain,prefix,scope,domains_json,variable_name,certificate_path,private_key_path,issuer,status,last_error,auto_renew,next_renew_at,not_before,not_after,created_at,updated_at`

func scanCertificate(row certScanner) (Certificate, error) {
	var cert Certificate
	var domains string
	var autoRenew int
	var nextRenewAt, notBefore, notAfter string
	var created, updated string
	if err := row.Scan(&cert.ID, &cert.Name, &cert.DomainID, &cert.Domain, &cert.Prefix, &cert.Scope, &domains, &cert.VariableName, &cert.CertificatePath, &cert.PrivateKeyPath, &cert.Issuer, &cert.Status, &cert.LastError, &autoRenew, &nextRenewAt, &notBefore, &notAfter, &created, &updated); err != nil {
		return Certificate{}, err
	}
	if domains != "" {
		_ = json.Unmarshal([]byte(domains), &cert.Domains)
	}
	cert.AutoRenew = autoRenew == 1
	cert.NextRenewAt = parseTime(nextRenewAt)
	cert.NotBefore = parseTime(notBefore)
	cert.NotAfter = parseTime(notAfter)
	cert.CreatedAt = parseTime(created)
	cert.UpdatedAt = parseTime(updated)
	return cert, nil
}

func (s *Service) resolveDomain(ctx context.Context, domainID string) (dns.ResolvedDomain, error) {
	if s.providerOverride != nil {
		if domainID == "" {
			domainID = "test-domain"
		}
		return dns.ResolvedDomain{Domain: dns.Domain{ID: domainID, Name: "example.com", Provider: dns.ProviderCloudflare}, APIToken: "test"}, nil
	}
	if s.domains == nil {
		return dns.ResolvedDomain{}, panelerr.BadGateway("certificate_provider_not_configured", "DNS domain service is not configured")
	}
	return s.domains.ResolveDomain(ctx, domainID)
}

func (s *Service) providerForDomain(domain dns.ResolvedDomain) (Provider, error) {
	if s.providerOverride != nil {
		return s.providerOverride, nil
	}
	switch domain.Provider {
	case dns.ProviderCloudflare:
		return NewACMEProvider(s.currentConfig(), dns.NewCloudflareProvider(domain.APIToken, nil), nil)
	default:
		return nil, panelerr.Validation("dns_provider_invalid", "Unsupported DNS provider")
	}
}

func nextRenewAt(notAfter time.Time) time.Time {
	if notAfter.IsZero() {
		return time.Now().UTC().Add(24 * time.Hour)
	}
	next := notAfter.Add(-30 * 24 * time.Hour)
	if next.Before(time.Now().UTC()) {
		return time.Now().UTC().Add(24 * time.Hour)
	}
	return next
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func formatOptionalTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return formatTime(value)
}

func parseTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	parsed, _ := time.Parse(time.RFC3339Nano, value)
	return parsed
}

type notConfiguredProvider struct{}

func (notConfiguredProvider) Issue(context.Context, Request) (Bundle, error) {
	return Bundle{}, panelerr.BadGateway("certificate_provider_not_configured", "Certificate provider is not configured")
}
