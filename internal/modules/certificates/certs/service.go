package certs

import (
	"context"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
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
	"panel/internal/platform/database/orm"
	panelerr "panel/internal/platform/errors"
	httpx "panel/internal/platform/http"
	id "panel/internal/platform/identity"
)

var (
	domainPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)
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

func (s *Service) Reissue(ctx context.Context, certID string, in IssueRequest) (IssueResult, error) {
	cert, err := s.Get(ctx, certID)
	if err != nil {
		return IssueResult{}, err
	}
	domainID := strings.TrimSpace(in.DomainID)
	if domainID == "" {
		domainID = cert.DomainID
	}
	resolved, err := s.resolveDomain(ctx, domainID)
	if err != nil {
		return IssueResult{}, err
	}
	in.DomainID = resolved.ID
	prepared, err := prepareIssueRequest(in, resolved.Name)
	if err != nil {
		return IssueResult{}, err
	}
	next := cert
	next.Name = prepared.Name
	next.DomainID = resolved.ID
	next.Domain = prepared.Domain
	next.Prefix = prepared.Prefix
	next.Scope = prepared.Scope
	next.Domains = prepared.Domains
	next.LastError = ""
	next.UpdatedAt = time.Now().UTC()
	taskID, err := s.recordAndRunTask(ctx, TaskTypeReissue, next, "Reissuing certificate for "+next.Domain)
	if err != nil {
		return IssueResult{}, err
	}
	return IssueResult{Certificate: next, TaskID: taskID}, nil
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
	certID := id.New("cert")
	cert := Certificate{
		ID:           certID,
		Name:         prepared.Name,
		DomainID:     resolved.ID,
		Domain:       prepared.Domain,
		Prefix:       prepared.Prefix,
		Scope:        prepared.Scope,
		Domains:      prepared.Domains,
		VariableName: certID,
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
	taskID, err := s.recordAndRunTask(ctx, TaskTypeIssue, cert, "Issue certificate for "+cert.Domain)
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
	cert, reissue, err := certificateFromIssueTask(task, cert)
	if err != nil {
		_ = s.tasks.Fail(ctx, task.ID, err)
		return err
	}
	if cert.Status == StatusIssued && !reissue {
		return s.tasks.Complete(ctx, task.ID, "Certificate already issued for "+cert.Domain)
	}
	if !reissue {
		if err := s.updateStatus(ctx, cert.ID, StatusIssuing, ""); err != nil {
			_ = s.tasks.Fail(ctx, task.ID, err)
			return err
		}
	} else if err := s.updateLastError(ctx, cert.ID, ""); err != nil {
		_ = s.tasks.Fail(ctx, task.ID, err)
		return err
	}
	if err := s.tasks.Advance(ctx, task.ID, "running", "Running ACME DNS-01 challenge"); err != nil {
		return err
	}
	if err := s.issueIntoCertificate(ctx, cert, task.ID); err != nil {
		if reissue {
			_ = s.updateLastError(ctx, cert.ID, err.Error())
		} else {
			_ = s.updateStatus(ctx, cert.ID, StatusFailed, err.Error())
		}
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
	if err := writeCertificateFiles(cert.CertificatePath, append(bundle.CertificatePEM, bundle.CAChainPEM...), cert.PrivateKeyPath, bundle.PrivateKeyPEM); err != nil {
		return err
	}
	cert.NotBefore, cert.NotAfter = certificateValidity(bundle.CertificatePEM)
	cert.NextRenewAt = nextRenewAt(cert.NotAfter)
	cert.LastError = ""
	cert.Status = StatusIssued
	cert.UpdatedAt = time.Now().UTC()
	return s.updateIssuedCertificate(ctx, cert)
}

func writeCertificateFiles(certificatePath string, certificatePEM []byte, privateKeyPath string, privateKeyPEM []byte) error {
	if fileExists(certificatePath) || fileExists(privateKeyPath) {
		return replaceCertificateFiles(certificatePath, certificatePEM, privateKeyPath, privateKeyPEM)
	}
	if err := os.WriteFile(certificatePath, certificatePEM, 0600); err != nil {
		return err
	}
	return os.WriteFile(privateKeyPath, privateKeyPEM, 0600)
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
	certBackedUp := false
	if err := os.Rename(certificatePath, certBackup); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
	} else {
		certBackedUp = true
	}
	defer func() {
		if certBackedUp {
			_ = os.Rename(certBackup, certificatePath)
		}
	}()
	keyBackedUp := false
	if err := os.Rename(privateKeyPath, keyBackup); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
	} else {
		keyBackedUp = true
	}
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

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
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
	rows, err := orm.Raw(ctx, s.db, `SELECT `+certificateColumns+` FROM certificates ORDER BY created_at DESC`)
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

func (s *Service) ListSummaries(ctx context.Context, page, pageSize int, query string) (httpx.ListPage[CertificateSummary], error) {
	filter := "1=1"
	args := []any{}
	if query != "" {
		filter = "(name LIKE ? ESCAPE '\\' OR domain LIKE ? ESCAPE '\\')"
		term := orm.LikeEscaped(query)
		args = append(args, term, term)
	}
	countQuery := orm.New(s.db).From("certificates")
	if query != "" {
		term := orm.LikeEscaped(query)
		countQuery.WhereGroup(func(c *orm.Condition) {
			c.Where("name LIKE ? ESCAPE '\\'", term)
			c.Or("domain LIKE ? ESCAPE '\\'", term)
		})
	}
	total64, err := countQuery.Count(ctx)
	if err != nil {
		return httpx.ListPage[CertificateSummary]{}, err
	}
	total := int(total64)
	listArgs := append(append([]any{}, args...), pageSize, (page-1)*pageSize)
	rows, err := orm.Raw(ctx, s.db, `SELECT id,name,domain_id,domain,prefix,scope,domains_json,issuer,status,last_error,auto_renew,COALESCE(next_renew_at,''),COALESCE(not_before,''),COALESCE(not_after,''),created_at,updated_at FROM certificates WHERE `+filter+` ORDER BY created_at DESC,id DESC LIMIT ? OFFSET ?`, listArgs...)
	if err != nil {
		return httpx.ListPage[CertificateSummary]{}, err
	}
	defer rows.Close()
	items := []CertificateSummary{}
	for rows.Next() {
		var item CertificateSummary
		var domains string
		var autoRenew int
		var nextRenewAt, notBefore, notAfter, createdAt, updatedAt string
		if err := rows.Scan(&item.ID, &item.Name, &item.DomainID, &item.Domain, &item.Prefix, &item.Scope, &domains, &item.Issuer, &item.Status, &item.LastError, &autoRenew, &nextRenewAt, &notBefore, &notAfter, &createdAt, &updatedAt); err != nil {
			return httpx.ListPage[CertificateSummary]{}, err
		}
		_ = json.Unmarshal([]byte(domains), &item.Domains)
		item.AutoRenew = autoRenew == 1
		item.NextRenewAt, item.NotBefore, item.NotAfter = parseTime(nextRenewAt), parseTime(notBefore), parseTime(notAfter)
		item.CreatedAt, item.UpdatedAt = parseTime(createdAt), parseTime(updatedAt)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return httpx.ListPage[CertificateSummary]{}, err
	}
	return httpx.ListPage[CertificateSummary]{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

func (s *Service) Get(ctx context.Context, certID string) (Certificate, error) {
	cert, err := scanCertificate(orm.RawRow(ctx, s.db, `SELECT `+certificateColumns+` FROM certificates WHERE id=?`, certID))
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
	res, err := orm.RawExec(ctx, s.db, `DELETE FROM certificates WHERE id=?`, certID)
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

func (s *Service) InternalFileCatalog(ctx context.Context) ([]applications.PanelFileDefinition, error) {
	certs, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]applications.PanelFileDefinition, 0, len(certs)*2)
	for _, cert := range certs {
		if cert.Status != StatusIssued {
			continue
		}
		for _, kind := range certificateInternalFileKinds() {
			out = append(out, applications.PanelFileDefinition{
				ID:           cert.ID + ":" + kind,
				ResourceID:   cert.ID,
				ResourceType: "certificate",
				Name:         cert.Name,
				Kind:         kind,
				Source:       "certificate:" + cert.ID + ":" + kind,
			})
		}
	}
	return out, nil
}

func (s *Service) OpenInternalFile(ctx context.Context, source string) (io.ReadCloser, applications.InternalFileInfo, error) {
	certID, kind, err := parseInternalFileSource(source)
	if err != nil {
		return nil, applications.InternalFileInfo{}, err
	}
	cert, err := s.Get(ctx, certID)
	if err != nil {
		return nil, applications.InternalFileInfo{}, err
	}
	if cert.Status != StatusIssued {
		return nil, applications.InternalFileInfo{}, panelerr.NotFound("certificate internal file")
	}
	path, filename, mode, err := certificateInternalFilePath(cert, kind)
	if err != nil {
		return nil, applications.InternalFileInfo{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, applications.InternalFileInfo{}, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, applications.InternalFileInfo{}, err
	}
	return file, applications.InternalFileInfo{Name: filename, Mode: mode, Size: info.Size()}, nil
}

func (s *Service) BuiltinVariables(ctx context.Context) (map[string]any, error) {
	certVars, err := s.ApplicationVariables(ctx, applications.ApplicationVariableContext{})
	if err != nil {
		return nil, err
	}
	return map[string]any{"certs": certVars}, nil
}

func (s *Service) ApplicationVariables(ctx context.Context, render applications.ApplicationVariableContext) (any, error) {
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
		certVar := map[string]any{
			"certificatePem":  string(certPEM),
			"privateKeyPem":   string(keyPEM),
			"certificate_pem": string(certPEM),
			"private_key_pem": string(keyPEM),
			"domains":         append([]string(nil), cert.Domains...),
		}
		certVars[cert.ID] = certVar
		if legacyKey := strings.TrimSpace(cert.VariableName); legacyKey != "" && legacyKey != cert.ID {
			certVars[legacyKey] = certVar
		}
	}
	return certVars, nil
}

func parseInternalFileSource(source string) (string, string, error) {
	parts := strings.Split(strings.TrimSpace(source), ":")
	if len(parts) != 3 || parts[0] != "certificate" || strings.TrimSpace(parts[1]) == "" {
		return "", "", panelerr.Validation("panel_file_source_invalid", "Panel file source is invalid")
	}
	kind := strings.TrimSpace(parts[2])
	switch kind {
	case "certificate", "private_key":
		return strings.TrimSpace(parts[1]), kind, nil
	default:
		return "", "", panelerr.Validation("panel_file_kind_invalid", "Certificate file kind is invalid")
	}
}

func certificateInternalFileKinds() []string {
	return []string{"certificate", "private_key"}
}

func certificateInternalFilePath(cert Certificate, kind string) (string, string, string, error) {
	base := strings.ReplaceAll(strings.TrimSpace(cert.Name), " ", "-")
	if base == "" {
		base = cert.ID
	}
	switch kind {
	case "certificate":
		return cert.CertificatePath, base + "-certificate.pem", "0644", nil
	case "private_key":
		return cert.PrivateKeyPath, base + "-private_key.pem", "0600", nil
	default:
		return "", "", "", panelerr.Validation("panel_file_kind_invalid", "Certificate file kind is invalid")
	}
}

func (s *Service) certificateInUse(ctx context.Context, certID string, domains []string, variableName string) (bool, error) {
	rows, err := orm.Raw(ctx, s.db, `SELECT spec_yaml FROM applications`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var spec string
		if err := rows.Scan(&spec); err != nil {
			return false, err
		}
		if certID != "" && strings.Contains(spec, "certificate:"+certID+":") {
			return true, nil
		}
		if certID != "" && strings.Contains(spec, ".certs."+certID) {
			return true, nil
		}
		if variableName != "" && strings.Contains(spec, ".certs."+variableName) {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	routeRows, err := orm.Raw(ctx, s.db, `SELECT domain FROM reverse_proxy_routes`)
	if err != nil {
		return false, err
	}
	defer routeRows.Close()
	for routeRows.Next() {
		var domain string
		if err := routeRows.Scan(&domain); err != nil {
			return false, err
		}
		for _, want := range domains {
			if certificateDomainMatches(want, domain) {
				return true, nil
			}
		}
	}
	return false, routeRows.Err()
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
		base := strings.TrimPrefix(pattern, "*.")
		return strings.HasSuffix(domain, "."+base) && strings.Count(domain, ".") == strings.Count(base, ".")+1
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
			Name:           cert.Name,
			Source:         "domain_certificate",
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
	_, err = orm.RawExec(ctx, s.db, `INSERT INTO certificates(id,name,domain_id,domain,prefix,scope,domains_json,variable_name,certificate_path,private_key_path,issuer,status,last_error,auto_renew,next_renew_at,not_before,not_after,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		cert.ID, cert.Name, cert.DomainID, cert.Domain, cert.Prefix, cert.Scope, string(domains), cert.VariableName, cert.CertificatePath, cert.PrivateKeyPath, cert.Issuer, cert.Status, cert.LastError, boolInt(cert.AutoRenew), formatOptionalTime(cert.NextRenewAt), formatOptionalTime(cert.NotBefore), formatOptionalTime(cert.NotAfter), formatTime(cert.CreatedAt), formatTime(cert.UpdatedAt))
	return err
}

func (s *Service) updateRenewal(ctx context.Context, cert Certificate) error {
	return orm.New(s.db).From("certificates").Where("id = ?", cert.ID).UpdateColumns(ctx, map[string]any{
		"not_before":    formatOptionalTime(cert.NotBefore),
		"not_after":     formatOptionalTime(cert.NotAfter),
		"next_renew_at": formatOptionalTime(cert.NextRenewAt),
		"last_error":    cert.LastError,
		"updated_at":    formatTime(cert.UpdatedAt),
	})
}

func (s *Service) updateIssuedCertificate(ctx context.Context, cert Certificate) error {
	domains, err := json.Marshal(cert.Domains)
	if err != nil {
		return err
	}
	return orm.New(s.db).From("certificates").Where("id = ?", cert.ID).UpdateColumns(ctx, map[string]any{
		"name":          cert.Name,
		"domain_id":     cert.DomainID,
		"domain":        cert.Domain,
		"prefix":        cert.Prefix,
		"scope":         cert.Scope,
		"domains_json":  string(domains),
		"variable_name": cert.VariableName,
		"issuer":        cert.Issuer,
		"status":        cert.Status,
		"last_error":    cert.LastError,
		"next_renew_at": formatOptionalTime(cert.NextRenewAt),
		"not_before":    formatOptionalTime(cert.NotBefore),
		"not_after":     formatOptionalTime(cert.NotAfter),
		"updated_at":    formatTime(cert.UpdatedAt),
	})
}

func (s *Service) updateStatus(ctx context.Context, certID, status, lastError string) error {
	return orm.New(s.db).From("certificates").Where("id = ?", certID).UpdateColumns(ctx, map[string]any{
		"status":     status,
		"last_error": lastError,
		"updated_at": formatTime(time.Now().UTC()),
	})
}

func (s *Service) updateLastError(ctx context.Context, certID, lastError string) error {
	return orm.New(s.db).From("certificates").Where("id = ?", certID).UpdateColumns(ctx, map[string]any{
		"last_error": lastError,
		"updated_at": formatTime(time.Now().UTC()),
	})
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

func (s *Service) recordAndRunTask(ctx context.Context, taskType string, cert Certificate, summary string) (string, error) {
	if s.tasks == nil {
		return "", nil
	}
	task, _, err := tasks.NewManager(s.tasks).CreateAndRun(ctx, tasks.CreateInput{
		Type:         taskType,
		ResourceType: "certificate",
		ResourceID:   cert.ID,
		Status:       tasks.StatusQueued,
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
		"name":          cert.Name,
		"domainId":      cert.DomainID,
		"domain":        cert.Domain,
		"prefix":        cert.Prefix,
		"scope":         cert.Scope,
		"domains":       cert.Domains,
		"issuer":        cert.Issuer,
	})
	if err != nil {
		return "{}"
	}
	return string(data)
}

func certificateFromIssueTask(task tasks.Task, cert Certificate) (Certificate, bool, error) {
	if task.Type != TaskTypeReissue {
		return cert, false, nil
	}
	var metadata struct {
		Name     string   `json:"name"`
		DomainID string   `json:"domainId"`
		Domain   string   `json:"domain"`
		Prefix   string   `json:"prefix"`
		Scope    string   `json:"scope"`
		Domains  []string `json:"domains"`
		Issuer   string   `json:"issuer"`
	}
	if err := json.Unmarshal([]byte(task.MetadataJSON), &metadata); err != nil {
		return Certificate{}, true, err
	}
	cert.Name = firstNonEmpty(strings.TrimSpace(metadata.Name), cert.Name)
	cert.DomainID = firstNonEmpty(strings.TrimSpace(metadata.DomainID), cert.DomainID)
	cert.Domain = firstNonEmpty(strings.TrimSpace(metadata.Domain), cert.Domain)
	cert.Prefix = strings.TrimSpace(metadata.Prefix)
	cert.Scope = firstNonEmpty(strings.TrimSpace(metadata.Scope), cert.Scope)
	if len(metadata.Domains) > 0 {
		cert.Domains = append([]string(nil), metadata.Domains...)
	}
	cert.Issuer = firstNonEmpty(strings.TrimSpace(metadata.Issuer), cert.Issuer)
	return cert, true, nil
}

func (s *Service) refreshApplications(ctx context.Context) error {
	if s.applications == nil {
		return nil
	}
	var joined error
	if _, err := s.applications.RedeployChangedApplications(ctx); err != nil {
		joined = errors.Join(joined, err)
	}
	if err := s.applications.ReconcileReverseProxy(ctx); err != nil {
		joined = errors.Join(joined, err)
	}
	return joined
}

type preparedIssueRequest struct {
	Name    string
	Domain  string
	Prefix  string
	Scope   string
	Domains []string
}

func prepareIssueRequest(in IssueRequest, managedDomain string) (preparedIssueRequest, error) {
	prefixes := normalizedIssuePrefixes(in)
	domains, err := domainsFromPrefixes(prefixes, managedDomain)
	if err != nil {
		return preparedIssueRequest{}, err
	}
	domain := primaryIssueDomain(domains)
	prefix := strings.Join(prefixes, ",")
	scope := ScopePrefixes
	if len(in.Prefixes) == 0 {
		scope = strings.TrimSpace(in.Scope)
		if scope == "" {
			scope = ScopeSingle
		}
		if scope != ScopeSingle && scope != ScopeWildcard {
			return preparedIssueRequest{}, panelerr.Validation("certificate_scope_invalid", "Certificate request scope is invalid")
		}
		if scope == ScopeWildcard {
			domains = []string{domain, "*." + domain}
		}
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = domain
	}
	return preparedIssueRequest{Name: name, Domain: domain, Prefix: prefix, Scope: scope, Domains: domains}, nil
}

func normalizedIssuePrefixes(in IssueRequest) []string {
	if len(in.Prefixes) == 0 {
		return []string{normalizePrefix(in.Prefix)}
	}
	prefixes := []string{}
	seen := map[string]bool{}
	for _, raw := range in.Prefixes {
		prefix := normalizePrefix(raw)
		if seen[prefix] {
			continue
		}
		seen[prefix] = true
		prefixes = append(prefixes, prefix)
	}
	if len(prefixes) == 0 {
		return []string{"@"}
	}
	return prefixes
}

func normalizePrefix(prefix string) string {
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	prefix = strings.TrimSuffix(prefix, ".")
	if prefix == "" {
		return "@"
	}
	return prefix
}

func domainsFromPrefixes(prefixes []string, managedDomain string) ([]string, error) {
	domains := make([]string, 0, len(prefixes))
	for _, prefix := range prefixes {
		domain := joinDomain(prefix, managedDomain)
		if !validCertificateDomain(domain) {
			return nil, panelerr.Validation("certificate_domain_invalid", "Domain must be a valid DNS name")
		}
		domains = append(domains, domain)
	}
	return domains, nil
}

func joinDomain(prefix, managedDomain string) string {
	managedDomain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(managedDomain), "."))
	if prefix == "@" {
		return managedDomain
	}
	return prefix + "." + managedDomain
}

func primaryIssueDomain(domains []string) string {
	if len(domains) == 0 {
		return ""
	}
	return strings.TrimPrefix(domains[0], "*.")
}

func validCertificateDomain(domain string) bool {
	if strings.Contains(domain, "*") {
		if !strings.HasPrefix(domain, "*.") || strings.Count(domain, "*") != 1 {
			return false
		}
		return domainPattern.MatchString(strings.TrimPrefix(domain, "*."))
	}
	return domainPattern.MatchString(domain)
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

const certificateColumns = `id,name,domain_id,domain,prefix,scope,domains_json,variable_name,certificate_path,private_key_path,issuer,status,last_error,auto_renew,COALESCE(next_renew_at,''),COALESCE(not_before,''),COALESCE(not_after,''),created_at,updated_at`

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

func formatOptionalTime(value time.Time) any {
	if value.IsZero() {
		return nil
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
