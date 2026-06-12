package keyassets

import (
	"context"
	"crypto"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"panel/internal/applications"
	"panel/internal/config"
	"panel/internal/id"
	"panel/internal/nomad"
	"panel/internal/panelerr"
	"panel/internal/secretstore"
	"panel/internal/tasks"
)

type applicationRefresher interface {
	RedeployEnabledApplications(ctx context.Context) (int, error)
	ReconcileReverseProxy(ctx context.Context) error
}

type Service struct {
	db           *sql.DB
	cfg          config.Config
	secrets      *secretstore.Store
	tasks        *tasks.Service
	applications applicationRefresher
	exportDir    string
	importDir    string

	planMu      sync.Mutex
	importPlans map[string]*importPlan
}

type storedAsset struct {
	Asset
	certificateText      string
	privateKeyCiphertext string
}

type importPlan struct {
	ID        string
	FilePath  string
	Assets    []storedAsset
	Conflicts []ImportConflict
	ExpiresAt time.Time
}

func NewService(db *sql.DB, cfg config.Config, secrets *secretstore.Store, taskSvc *tasks.Service) *Service {
	tmpRoot := filepath.Join(cfg.DataRoot, "tmp", "key-assets")
	return &Service{
		db:          db,
		cfg:         cfg,
		secrets:     secrets,
		tasks:       taskSvc,
		exportDir:   filepath.Join(cfg.DataRoot, "tmp", "key-asset-exports"),
		importDir:   filepath.Join(tmpRoot, "imports"),
		importPlans: map[string]*importPlan{},
	}
}

func (s *Service) SetApplicationRefresher(refresher applicationRefresher) {
	s.applications = refresher
}

func (s *Service) EnsureLegacySelfSignedMigrated(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO runtime_settings(key,value,updated_at) VALUES('key_assets_self_signed_migrated','',?)`, formatTime(time.Now().UTC())); err != nil {
		return err
	}
	var marker string
	if err := s.db.QueryRowContext(ctx, `SELECT value FROM runtime_settings WHERE key='key_assets_self_signed_migrated'`).Scan(&marker); err != nil {
		return err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,parent_ca_id,kind,name,common_name,dns_names_json,ip_addresses_json,certificate_path,private_key_path,public_key_path,created_at,updated_at FROM self_signed_certificates ORDER BY kind,name`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type legacyRecord struct {
		ID           string
		ParentCAID   string
		Kind         string
		Name         string
		CommonName   string
		DNSJSON      string
		IPJSON       string
		CertPath     string
		KeyPath      string
		PublicPath   string
		CreatedAtRaw string
		UpdatedAtRaw string
	}
	records := []legacyRecord{}
	for rows.Next() {
		var rec legacyRecord
		if err := rows.Scan(&rec.ID, &rec.ParentCAID, &rec.Kind, &rec.Name, &rec.CommonName, &rec.DNSJSON, &rec.IPJSON, &rec.CertPath, &rec.KeyPath, &rec.PublicPath, &rec.CreatedAtRaw, &rec.UpdatedAtRaw); err != nil {
			return err
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(records) == 0 {
		if marker != "done" {
			_, err = s.db.ExecContext(ctx, `UPDATE runtime_settings SET value='done',updated_at=? WHERE key='key_assets_self_signed_migrated'`, formatTime(time.Now().UTC()))
		}
		return err
	}
	toCleanup := []string{}
	preparedAssets := []storedAsset{}
	for _, rec := range records {
		existing, err := s.getStoredAsset(ctx, rec.ID)
		if err == nil && existing.ID != "" {
			if rec.CertPath != "" {
				toCleanup = append(toCleanup, filepath.Dir(rec.CertPath))
			}
			continue
		}
		certPEM, err := os.ReadFile(rec.CertPath)
		if err != nil {
			return fmt.Errorf("legacy self-signed asset %s certificate: %w", rec.ID, err)
		}
		keyPEM, err := os.ReadFile(rec.KeyPath)
		if err != nil {
			return fmt.Errorf("legacy self-signed asset %s private key: %w", rec.ID, err)
		}
		publicPEM, err := os.ReadFile(rec.PublicPath)
		if err != nil {
			return fmt.Errorf("legacy self-signed asset %s public key: %w", rec.ID, err)
		}
		var dnsNames, ipAddresses []string
		_ = json.Unmarshal([]byte(rec.DNSJSON), &dnsNames)
		_ = json.Unmarshal([]byte(rec.IPJSON), &ipAddresses)
		createdAt := parseTime(rec.CreatedAtRaw)
		if createdAt.IsZero() {
			createdAt = time.Now().UTC()
		}
		updatedAt := parseTime(rec.UpdatedAtRaw)
		if updatedAt.IsZero() {
			updatedAt = createdAt
		}
		privateKey, encodedPrivate, err := parsePrivateKeyPEM(string(keyPEM))
		if err != nil {
			return fmt.Errorf("legacy self-signed asset %s private key invalid: %w", rec.ID, err)
		}
		publicKeyText, publicKey, err := parsePublicKeyText(string(publicPEM))
		if err != nil {
			return fmt.Errorf("legacy self-signed asset %s public key invalid: %w", rec.ID, err)
		}
		if err := ensurePublicKeyMatches(privateKey.public, publicKey); err != nil {
			return fmt.Errorf("legacy self-signed asset %s key pair mismatch: %w", rec.ID, err)
		}
		cert, normalizedCertPEM, err := parseCertificatePEM(string(certPEM))
		if err != nil {
			return fmt.Errorf("legacy self-signed asset %s certificate invalid: %w", rec.ID, err)
		}
		if err := ensurePublicKeyMatches(privateKey.public, cert.PublicKey); err != nil {
			return fmt.Errorf("legacy self-signed asset %s certificate does not match key pair: %w", rec.ID, err)
		}
		assetType := TypeTLSCertificate
		if rec.Kind == "ca" {
			assetType = TypeCACertificate
			if !cert.IsCA {
				return panelerr.Validation("key_asset_ca_invalid", "Legacy self-signed CA certificate is invalid")
			}
		}
		stored := storedAsset{
			Asset: Asset{
				ID:            rec.ID,
				Type:          assetType,
				Name:          rec.Name,
				ParentAssetID: strings.TrimSpace(rec.ParentCAID),
				Algorithm:     privateKey.algorithm,
				KeySize:       privateKey.keySize,
				CommonName:    rec.CommonName,
				DNSNames:      dnsNames,
				IPAddresses:   ipAddresses,
				Fingerprint:   certificateFingerprint(cert),
				PublicKey:     publicKeyText,
				Metadata:      map[string]any{"legacySource": "self_signed"},
				NotBefore:     cert.NotBefore,
				NotAfter:      cert.NotAfter,
				CreatedAt:     createdAt,
				UpdatedAt:     updatedAt,
			},
			certificateText:      string(normalizedCertPEM),
			privateKeyCiphertext: string(encodedPrivate),
		}
		preparedAssets = append(preparedAssets, stored)
		toCleanup = append(toCleanup, filepath.Dir(rec.CertPath))
	}
	if err := withTx(ctx, s.db, func(tx *sql.Tx) error {
		for _, asset := range preparedAssets {
			var exists int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM key_assets WHERE id=?`, asset.ID).Scan(&exists); err != nil {
				return err
			}
			if exists > 0 {
				continue
			}
			if err := s.insertAssetTx(ctx, tx, asset); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `UPDATE runtime_settings SET value='done',updated_at=? WHERE key='key_assets_self_signed_migrated'`, formatTime(time.Now().UTC()))
		return err
	}); err != nil {
		return err
	}
	for _, dir := range toCleanup {
		if dir == "" {
			continue
		}
		_ = os.RemoveAll(dir)
	}
	return nil
}

func (s *Service) List(ctx context.Context) ([]Asset, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+assetColumns+` FROM key_assets ORDER BY type,name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	references, err := s.assetReferences(ctx)
	if err != nil {
		return nil, err
	}
	childCounts, err := s.childCounts(ctx)
	if err != nil {
		return nil, err
	}
	assets := []Asset{}
	for rows.Next() {
		stored, err := scanStoredAsset(rows)
		if err != nil {
			return nil, err
		}
		asset := decorateAsset(stored.Asset, references[stored.ID], childCounts[stored.ID])
		assets = append(assets, asset)
	}
	return assets, rows.Err()
}

func (s *Service) Get(ctx context.Context, assetID string) (Asset, error) {
	stored, err := s.getStoredAsset(ctx, assetID)
	if err != nil {
		return Asset{}, err
	}
	references, err := s.assetReferences(ctx)
	if err != nil {
		return Asset{}, err
	}
	childCounts, err := s.childCounts(ctx)
	if err != nil {
		return Asset{}, err
	}
	return decorateAsset(stored.Asset, references[assetID], childCounts[assetID]), nil
}

func (s *Service) CreateCA(ctx context.Context, in CreateCARequest) (Asset, error) {
	name := strings.TrimSpace(in.Name)
	commonName := strings.TrimSpace(in.CommonName)
	if name == "" || commonName == "" {
		return Asset{}, panelerr.Validation("key_asset_type_invalid", "CA name and common name are required")
	}
	validityDays := in.ValidityDays
	if validityDays <= 0 {
		if in.Years > 0 {
			validityDays = in.Years * 365
		} else {
			validityDays = 3650
		}
	}
	key, err := generateKeyMaterial(in.Algorithm, in.KeySize)
	if err != nil {
		return Asset{}, err
	}
	privateKeyPEM, err := marshalPrivateKeyPEM(key.private)
	if err != nil {
		return Asset{}, err
	}
	template := newCertificateTemplate(commonName, time.Now().UTC().Add(-time.Hour), time.Now().UTC().Add(time.Duration(validityDays)*24*time.Hour), true, nil, nil)
	certificatePEM, err := generateCertificate(template, template, key.public, key.private)
	if err != nil {
		return Asset{}, err
	}
	cert, normalizedCertPEM, err := parseCertificatePEM(string(certificatePEM))
	if err != nil {
		return Asset{}, err
	}
	material, err := buildCertificateMaterial(cert, normalizedCertPEM, key, privateKeyPEM)
	if err != nil {
		return Asset{}, err
	}
	asset := storedAsset{
		Asset: Asset{
			ID:          id.New("ca"),
			Type:        TypeCACertificate,
			Name:        name,
			Algorithm:   key.algorithm,
			KeySize:     key.keySize,
			CommonName:  commonName,
			Fingerprint: certificateFingerprint(cert),
			PublicKey:   publicKeyDisplayForCertificate(material.publicKeyPEM),
			Metadata:    map[string]any{"origin": "generated"},
			NotBefore:   cert.NotBefore,
			NotAfter:    cert.NotAfter,
			CreatedAt:   time.Now().UTC(),
			UpdatedAt:   time.Now().UTC(),
		},
		certificateText:      string(material.certificatePEM),
		privateKeyCiphertext: string(material.privateKeyPEM),
	}
	if err := s.insertAsset(ctx, asset); err != nil {
		return Asset{}, err
	}
	return s.Get(ctx, asset.ID)
}

func (s *Service) CreateTLS(ctx context.Context, in CreateTLSRequest) (Asset, error) {
	parentID := firstNonEmpty(in.CAID, in.ParentAssetID)
	parent, err := s.getStoredAsset(ctx, parentID)
	if err != nil {
		return Asset{}, err
	}
	if parent.Type != TypeCACertificate {
		return Asset{}, panelerr.Validation("key_asset_ca_invalid", "Selected parent asset is not a CA certificate")
	}
	dnsNames := normalizeDNSNames(in.DNSNames)
	ipStrings, ips, err := parseIPs(in.IPAddresses)
	if err != nil {
		return Asset{}, err
	}
	if len(dnsNames) == 0 && len(ipStrings) == 0 {
		return Asset{}, panelerr.Validation("key_asset_type_invalid", "TLS certificate requires at least one DNS name or IP address")
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = defaultTLSCommonName("", dnsNames, ipStrings)
	}
	commonName := strings.TrimSpace(in.CommonName)
	if commonName == "" {
		commonName = defaultTLSCommonName(name, dnsNames, ipStrings)
	}
	key, err := generateKeyMaterial(in.Algorithm, in.KeySize)
	if err != nil {
		return Asset{}, err
	}
	privateKeyPEM, err := marshalPrivateKeyPEM(key.private)
	if err != nil {
		return Asset{}, err
	}
	parentCert, err := parent.certificate()
	if err != nil {
		return Asset{}, err
	}
	parentPrivateKey, err := s.decryptPrivateKey(parent)
	if err != nil {
		return Asset{}, err
	}
	validityDays := in.ValidityDays
	if validityDays <= 0 {
		if in.Days > 0 {
			validityDays = in.Days
		} else {
			validityDays = 365
		}
	}
	template := newCertificateTemplate(commonName, time.Now().UTC().Add(-time.Hour), time.Now().UTC().Add(time.Duration(validityDays)*24*time.Hour), false, dnsNames, ips)
	certificatePEM, err := generateCertificate(template, parentCert, key.public, parentPrivateKey)
	if err != nil {
		return Asset{}, err
	}
	cert, normalizedCertPEM, err := parseCertificatePEM(string(certificatePEM))
	if err != nil {
		return Asset{}, err
	}
	material, err := buildCertificateMaterial(cert, normalizedCertPEM, key, privateKeyPEM)
	if err != nil {
		return Asset{}, err
	}
	asset := storedAsset{
		Asset: Asset{
			ID:            id.New("cert"),
			Type:          TypeTLSCertificate,
			Name:          name,
			ParentAssetID: parent.ID,
			Algorithm:     key.algorithm,
			KeySize:       key.keySize,
			CommonName:    commonName,
			DNSNames:      dnsNames,
			IPAddresses:   ipStrings,
			Fingerprint:   certificateFingerprint(cert),
			PublicKey:     publicKeyDisplayForCertificate(material.publicKeyPEM),
			Metadata:      map[string]any{"origin": "generated"},
			NotBefore:     cert.NotBefore,
			NotAfter:      cert.NotAfter,
			CreatedAt:     time.Now().UTC(),
			UpdatedAt:     time.Now().UTC(),
		},
		certificateText:      string(material.certificatePEM),
		privateKeyCiphertext: string(material.privateKeyPEM),
	}
	if err := s.insertAsset(ctx, asset); err != nil {
		return Asset{}, err
	}
	return s.Get(ctx, asset.ID)
}

func (s *Service) GenerateSSH(ctx context.Context, in GenerateSSHRequest) (Asset, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return Asset{}, panelerr.Validation("key_asset_type_invalid", "SSH key pair name is required")
	}
	key, err := generateKeyMaterial(in.Algorithm, in.KeySize)
	if err != nil {
		return Asset{}, err
	}
	privateKeyPEM, err := marshalPrivateKeyPEM(key.private)
	if err != nil {
		return Asset{}, err
	}
	material, err := buildSSHMaterial(key, privateKeyPEM, in.Comment)
	if err != nil {
		return Asset{}, err
	}
	sshPublic, _, _, _, err := ssh.ParseAuthorizedKey([]byte(material.publicKeyText))
	if err != nil {
		return Asset{}, err
	}
	asset := storedAsset{
		Asset: Asset{
			ID:          id.New("ssh"),
			Type:        TypeSSHKeyPair,
			Name:        name,
			Algorithm:   key.algorithm,
			KeySize:     key.keySize,
			Fingerprint: sshFingerprint(sshPublic),
			PublicKey:   strings.TrimSpace(material.publicKeyText),
			Metadata:    map[string]any{"origin": "generated"},
			CreatedAt:   time.Now().UTC(),
			UpdatedAt:   time.Now().UTC(),
		},
		privateKeyCiphertext: string(material.privateKeyPEM),
	}
	if err := s.insertAsset(ctx, asset); err != nil {
		return Asset{}, err
	}
	return s.Get(ctx, asset.ID)
}

func (s *Service) Import(ctx context.Context, in ImportRequest) (Asset, error) {
	stored, err := s.prepareImportedAsset(ctx, in, "")
	if err != nil {
		return Asset{}, err
	}
	stored.Asset.ID = id.New(importIDPrefix(stored.Type))
	stored.Asset.CreatedAt = time.Now().UTC()
	stored.Asset.UpdatedAt = stored.Asset.CreatedAt
	if err := s.insertAsset(ctx, stored); err != nil {
		return Asset{}, err
	}
	return s.Get(ctx, stored.ID)
}

func (s *Service) Delete(ctx context.Context, assetID string) error {
	stored, err := s.getStoredAsset(ctx, assetID)
	if err != nil {
		return err
	}
	if stored.Type == TypeCACertificate {
		var children int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM key_assets WHERE parent_asset_id=?`, assetID).Scan(&children); err != nil {
			return err
		}
		if children > 0 {
			return panelerr.Conflict("key_asset_ca_has_children", "Certificate authority still has child certificates")
		}
	}
	references, err := s.assetReferences(ctx)
	if err != nil {
		return err
	}
	if len(references[assetID]) > 0 {
		return panelerr.Conflict("key_asset_in_use", "Key asset is still used by an application or reverse proxy")
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM key_assets WHERE id=?`, assetID)
	return err
}

func (s *Service) ReissueTLS(ctx context.Context, assetID string) (ReissueResult, error) {
	stored, err := s.getStoredAsset(ctx, assetID)
	if err != nil {
		return ReissueResult{}, err
	}
	if stored.Type != TypeTLSCertificate {
		return ReissueResult{}, panelerr.Validation("key_asset_type_invalid", "Only TLS certificates can be reissued")
	}
	if strings.TrimSpace(stored.ParentAssetID) == "" {
		return ReissueResult{}, panelerr.Validation("key_asset_ca_required", "Standalone TLS certificates cannot be reissued without a CA")
	}
	taskID, fail, complete, err := s.beginAssetTask(ctx, TaskTypeTLSReissue, stored.ID, "Reissuing TLS certificate "+stored.Name)
	if err != nil {
		return ReissueResult{}, err
	}
	parent, err := s.getStoredAsset(ctx, stored.ParentAssetID)
	if err != nil {
		_ = fail(err)
		return ReissueResult{}, err
	}
	parentCert, err := parent.certificate()
	if err != nil {
		_ = fail(err)
		return ReissueResult{}, err
	}
	parentPrivateKey, err := s.decryptPrivateKey(parent)
	if err != nil {
		_ = fail(err)
		return ReissueResult{}, err
	}
	key, err := generateKeyMaterial(stored.Algorithm, stored.KeySize)
	if err != nil {
		_ = fail(err)
		return ReissueResult{}, err
	}
	privateKeyPEM, err := marshalPrivateKeyPEM(key.private)
	if err != nil {
		_ = fail(err)
		return ReissueResult{}, err
	}
	_, ips, err := parseIPs(stored.IPAddresses)
	if err != nil {
		_ = fail(err)
		return ReissueResult{}, err
	}
	duration := stored.NotAfter.Sub(stored.NotBefore)
	if duration <= 0 {
		duration = 365 * 24 * time.Hour
	}
	template := newCertificateTemplate(stored.CommonName, time.Now().UTC().Add(-time.Hour), time.Now().UTC().Add(duration), false, stored.DNSNames, ips)
	certificatePEM, err := generateCertificate(template, parentCert, key.public, parentPrivateKey)
	if err != nil {
		_ = fail(err)
		return ReissueResult{}, err
	}
	cert, normalizedCertPEM, err := parseCertificatePEM(string(certificatePEM))
	if err != nil {
		_ = fail(err)
		return ReissueResult{}, err
	}
	publicKeyPEM, err := marshalPublicKeyPEM(key.public)
	if err != nil {
		_ = fail(err)
		return ReissueResult{}, err
	}
	stored.PublicKey = string(publicKeyPEM)
	stored.Fingerprint = certificateFingerprint(cert)
	stored.NotBefore = cert.NotBefore
	stored.NotAfter = cert.NotAfter
	stored.UpdatedAt = time.Now().UTC()
	stored.certificateText = string(normalizedCertPEM)
	stored.privateKeyCiphertext = string(privateKeyPEM)
	if err := s.updateAsset(ctx, stored); err != nil {
		_ = fail(err)
		return ReissueResult{}, err
	}
	if err := s.refreshApplications(ctx); err != nil {
		_ = fail(err)
		return ReissueResult{}, err
	}
	if err := complete("Reissued TLS certificate " + stored.Name); err != nil {
		return ReissueResult{}, err
	}
	asset, err := s.Get(ctx, stored.ID)
	return ReissueResult{Asset: asset, TaskID: taskID}, err
}

func (s *Service) RegenerateSSH(ctx context.Context, assetID string) (RegenerateResult, error) {
	stored, err := s.getStoredAsset(ctx, assetID)
	if err != nil {
		return RegenerateResult{}, err
	}
	if stored.Type != TypeSSHKeyPair {
		return RegenerateResult{}, panelerr.Validation("key_asset_type_invalid", "Only SSH key pairs can be regenerated")
	}
	taskID, fail, complete, err := s.beginAssetTask(ctx, TaskTypeSSHRegenerate, stored.ID, "Regenerating SSH key pair "+stored.Name)
	if err != nil {
		return RegenerateResult{}, err
	}
	key, err := generateKeyMaterial(stored.Algorithm, stored.KeySize)
	if err != nil {
		_ = fail(err)
		return RegenerateResult{}, err
	}
	privateKeyPEM, err := marshalPrivateKeyPEM(key.private)
	if err != nil {
		_ = fail(err)
		return RegenerateResult{}, err
	}
	material, err := buildSSHMaterial(key, privateKeyPEM, "")
	if err != nil {
		_ = fail(err)
		return RegenerateResult{}, err
	}
	sshPublic, _, _, _, err := ssh.ParseAuthorizedKey([]byte(material.publicKeyText))
	if err != nil {
		_ = fail(err)
		return RegenerateResult{}, err
	}
	stored.PublicKey = strings.TrimSpace(material.publicKeyText)
	stored.Fingerprint = sshFingerprint(sshPublic)
	stored.UpdatedAt = time.Now().UTC()
	stored.privateKeyCiphertext = string(material.privateKeyPEM)
	if err := s.updateAsset(ctx, stored); err != nil {
		_ = fail(err)
		return RegenerateResult{}, err
	}
	if err := s.refreshApplications(ctx); err != nil {
		_ = fail(err)
		return RegenerateResult{}, err
	}
	if err := complete("Regenerated SSH key pair " + stored.Name); err != nil {
		return RegenerateResult{}, err
	}
	asset, err := s.Get(ctx, stored.ID)
	return RegenerateResult{Asset: asset, TaskID: taskID}, err
}

func (s *Service) ReadFile(ctx context.Context, assetID, kind string) ([]byte, string, error) {
	stored, err := s.getStoredAsset(ctx, assetID)
	if err != nil {
		return nil, "", err
	}
	kind = strings.TrimSpace(kind)
	switch stored.Type {
	case TypeCACertificate, TypeTLSCertificate:
		switch kind {
		case "certificate":
			return []byte(stored.certificateText), certificateFileName(stored.Asset, kind), nil
		case "private_key":
			privateKeyPEM, err := s.decryptPrivateKeyPEM(stored)
			return privateKeyPEM, certificateFileName(stored.Asset, kind), err
		case "public_key":
			return []byte(stored.PublicKey), certificateFileName(stored.Asset, kind), nil
		}
	case TypeSSHKeyPair:
		switch kind {
		case "private_key":
			privateKeyPEM, err := s.decryptPrivateKeyPEM(stored)
			return privateKeyPEM, certificateFileName(stored.Asset, kind), err
		case "ssh_public_key", "public_key":
			return []byte(strings.TrimSpace(stored.PublicKey) + "\n"), certificateFileName(stored.Asset, "public_key"), nil
		}
	}
	return nil, "", panelerr.Validation("panel_file_kind_invalid", "Key asset file kind is invalid")
}

func (s *Service) PanelFileCatalog(ctx context.Context) ([]applications.PanelFileDefinition, error) {
	assets, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]applications.PanelFileDefinition, 0, len(assets)*3)
	for _, asset := range assets {
		for _, kind := range fileKindsForAsset(asset.Type) {
			out = append(out, applications.PanelFileDefinition{
				ID:           asset.ID + ":" + kind,
				ResourceID:   asset.ID,
				ResourceType: "key_asset",
				Name:         asset.Name,
				Kind:         kind,
				Source:       "key_asset:" + asset.ID + ":" + kind,
			})
		}
	}
	return out, nil
}

func (s *Service) ReadPanelFile(ctx context.Context, source string) ([]byte, error) {
	assetID, kind, err := s.parsePanelFileSource(ctx, source)
	if err != nil {
		return nil, err
	}
	content, _, err := s.ReadFile(ctx, assetID, kind)
	return content, err
}

func (s *Service) ReverseProxyCertificates(ctx context.Context) ([]nomad.ReverseProxyCertificate, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+assetColumns+` FROM key_assets WHERE type=? ORDER BY name`, TypeTLSCertificate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []nomad.ReverseProxyCertificate{}
	for rows.Next() {
		stored, err := scanStoredAsset(rows)
		if err != nil {
			return nil, err
		}
		privateKeyPEM, err := s.decryptPrivateKeyPEM(stored)
		if err != nil {
			return nil, err
		}
		domains := append([]string(nil), stored.DNSNames...)
		domains = append(domains, stored.IPAddresses...)
		out = append(out, nomad.ReverseProxyCertificate{
			ID:             stored.ID,
			Domains:        domains,
			CertificatePEM: stored.certificateText,
			PrivateKeyPEM:  string(privateKeyPEM),
		})
	}
	return out, rows.Err()
}

func (s *Service) CreateExport(ctx context.Context, in ExportRequest) (ExportResult, error) {
	if strings.TrimSpace(in.Password) == "" || len(in.Password) < 12 {
		return ExportResult{}, panelerr.Validation("key_asset_archive_password_invalid", "Archive password is invalid")
	}
	taskID, fail, complete, err := s.beginAssetTask(ctx, TaskTypeExport, "", "Exporting key assets")
	if err != nil {
		return ExportResult{}, err
	}
	payload := archivePayload{Assets: []archiveAsset{}}
	seen := map[string]struct{}{}
	for _, assetID := range in.AssetIDs {
		assetID = strings.TrimSpace(assetID)
		if assetID == "" {
			continue
		}
		if _, ok := seen[assetID]; ok {
			continue
		}
		seen[assetID] = struct{}{}
		stored, err := s.getStoredAsset(ctx, assetID)
		if err != nil {
			_ = fail(err)
			return ExportResult{}, err
		}
		privateKeyPEM, err := s.decryptPrivateKeyPEM(stored)
		if err != nil {
			_ = fail(err)
			return ExportResult{}, err
		}
		payload.Assets = append(payload.Assets, archiveAsset{
			ID:             stored.ID,
			Type:           stored.Type,
			Name:           stored.Name,
			ParentAssetID:  stored.ParentAssetID,
			Algorithm:      stored.Algorithm,
			KeySize:        stored.KeySize,
			CommonName:     stored.CommonName,
			DNSNames:       append([]string(nil), stored.DNSNames...),
			IPAddresses:    append([]string(nil), stored.IPAddresses...),
			Metadata:       cloneMetadata(stored.Metadata),
			CertificatePEM: stored.certificateText,
			PrivateKeyPEM:  string(privateKeyPEM),
			PublicKey:      stored.PublicKey,
		})
	}
	sort.Slice(payload.Assets, func(i, j int) bool { return payload.Assets[i].ID < payload.Assets[j].ID })
	archiveBytes, err := encryptArchive(in.Password, payload)
	if err != nil {
		_ = fail(err)
		return ExportResult{}, err
	}
	if err := os.MkdirAll(s.exportDir, 0o700); err != nil {
		_ = fail(err)
		return ExportResult{}, err
	}
	filename := taskID + ".panel-key-assets"
	filePath := filepath.Join(s.exportDir, filename)
	if err := os.WriteFile(filePath, archiveBytes, 0o600); err != nil {
		_ = fail(err)
		return ExportResult{}, err
	}
	now := time.Now().UTC()
	expiresAt := now.Add(30 * time.Minute)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO key_asset_exports(task_id,filename,file_path,expires_at,created_at,updated_at) VALUES(?,?,?,?,?,?)
		ON CONFLICT(task_id) DO UPDATE SET filename=excluded.filename,file_path=excluded.file_path,expires_at=excluded.expires_at,updated_at=excluded.updated_at`,
		taskID, filename, filePath, formatTime(expiresAt), formatTime(now), formatTime(now)); err != nil {
		_ = fail(err)
		return ExportResult{}, err
	}
	if err := complete("Exported key assets"); err != nil {
		return ExportResult{}, err
	}
	return ExportResult{TaskID: taskID}, nil
}

func (s *Service) DownloadExport(ctx context.Context, taskID string) ([]byte, string, error) {
	if s.tasks != nil {
		task, err := s.tasks.Get(ctx, taskID)
		if err != nil {
			return nil, "", err
		}
		if task.Type != TaskTypeExport {
			return nil, "", panelerr.NotFound("key_asset_export")
		}
		if task.Status != tasks.StatusCompleted {
			return nil, "", panelerr.NotFound("key_asset_export")
		}
	}
	var filename, filePath, expiresAtRaw string
	if err := s.db.QueryRowContext(ctx, `SELECT filename,file_path,expires_at FROM key_asset_exports WHERE task_id=?`, taskID).Scan(&filename, &filePath, &expiresAtRaw); err != nil {
		if err == sql.ErrNoRows {
			return nil, "", panelerr.NotFound("key_asset_export")
		}
		return nil, "", err
	}
	if expiresAt := parseTime(expiresAtRaw); !expiresAt.IsZero() && time.Now().UTC().After(expiresAt) {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM key_asset_exports WHERE task_id=?`, taskID)
		_ = os.Remove(filePath)
		return nil, "", panelerr.NotFound("key_asset_export")
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, "", panelerr.NotFound("key_asset_export")
		}
		return nil, "", err
	}
	return content, filename, nil
}

func (s *Service) PreflightImport(ctx context.Context, in ImportPreflightRequest) (ImportPreflightResult, error) {
	archiveBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(in.ArchiveBase64))
	if err != nil {
		return ImportPreflightResult{}, panelerr.Validation("key_asset_archive_tampered", "Key asset archive is invalid")
	}
	payload, err := decryptArchive(archiveBytes, in.Password)
	if err != nil {
		return ImportPreflightResult{}, err
	}
	now := time.Now().UTC()
	assets := make([]storedAsset, 0, len(payload.Assets))
	conflicts := []ImportConflict{}
	overwriteInUse := []string{}
	for _, item := range payload.Assets {
		stored, err := s.prepareImportedAsset(ctx, ImportRequest{
			Type:           item.Type,
			Name:           item.Name,
			ParentAssetID:  item.ParentAssetID,
			CommonName:     item.CommonName,
			Algorithm:      item.Algorithm,
			KeySize:        item.KeySize,
			CertificatePEM: item.CertificatePEM,
			PrivateKeyPEM:  item.PrivateKeyPEM,
			PublicKey:      item.PublicKey,
		}, item.ID)
		if err != nil {
			return ImportPreflightResult{}, err
		}
		stored.Metadata = cloneMetadata(item.Metadata)
		stored.CreatedAt = now
		stored.UpdatedAt = now
		assets = append(assets, stored)
		if existingByID, err := s.getStoredAsset(ctx, item.ID); err == nil {
			conflicts = append(conflicts, ImportConflict{IncomingID: item.ID, IncomingName: item.Name, ExistingID: existingByID.ID, ExistingName: existingByID.Name, ConflictByID: true})
			if used, err := s.assetInUse(ctx, existingByID.ID); err == nil && used {
				overwriteInUse = append(overwriteInUse, existingByID.ID)
			}
		}
		existingByName, err := s.getStoredAssetByName(ctx, item.Name)
		if err == nil && existingByName.ID != item.ID {
			conflicts = append(conflicts, ImportConflict{IncomingID: item.ID, IncomingName: item.Name, ExistingID: existingByName.ID, ExistingName: existingByName.Name, ConflictByName: true})
		}
	}
	planID := id.New("kimport")
	if err := os.MkdirAll(s.importDir, 0o700); err != nil {
		return ImportPreflightResult{}, err
	}
	planPath := filepath.Join(s.importDir, planID+".json")
	if err := os.WriteFile(planPath, archiveBytes, 0o600); err != nil {
		return ImportPreflightResult{}, err
	}
	expiresAt := now.Add(30 * time.Minute)
	s.planMu.Lock()
	s.importPlans[planID] = &importPlan{ID: planID, FilePath: planPath, Assets: assets, Conflicts: conflicts, ExpiresAt: expiresAt}
	s.planMu.Unlock()
	resultAssets := make([]ImportAssetCheck, 0, len(assets))
	for _, asset := range assets {
		resultAssets = append(resultAssets, ImportAssetCheck{
			ID:            asset.ID,
			Type:          asset.Type,
			Name:          asset.Name,
			ParentAssetID: asset.ParentAssetID,
			Algorithm:     asset.Algorithm,
			KeySize:       asset.KeySize,
			CommonName:    asset.CommonName,
			Fingerprint:   asset.Fingerprint,
			StandaloneTLS: asset.Type == TypeTLSCertificate && strings.TrimSpace(asset.ParentAssetID) == "",
		})
	}
	sort.Strings(overwriteInUse)
	return ImportPreflightResult{
		PlanID:         planID,
		ExpiresAt:      expiresAt,
		Assets:         resultAssets,
		Conflicts:      dedupeConflicts(conflicts),
		OverwriteInUse: dedupeStrings(overwriteInUse),
	}, nil
}

func (s *Service) ExecuteImport(ctx context.Context, planID string, in ImportExecuteRequest) (ImportExecuteResult, error) {
	plan, err := s.getImportPlan(planID)
	if err != nil {
		return ImportExecuteResult{}, err
	}
	taskID, fail, complete, err := s.beginAssetTask(ctx, TaskTypeImport, "", "Importing key assets")
	if err != nil {
		return ImportExecuteResult{}, err
	}
	strategy := strings.TrimSpace(in.Strategy)
	if strategy == "" {
		strategy = "skip_existing"
	}
	if strategy != "skip_existing" && strategy != "generate_new_id" && strategy != "overwrite_existing" {
		_ = fail(panelerr.Validation("key_asset_import_conflict", "Import conflict strategy is invalid"))
		return ImportExecuteResult{}, panelerr.Validation("key_asset_import_conflict", "Import conflict strategy is invalid")
	}
	if strategy == "overwrite_existing" {
		for _, conflict := range plan.Conflicts {
			if conflict.ConflictByName && conflict.ExistingID != conflict.IncomingID {
				err := panelerr.Validation("key_asset_import_conflict", "Name conflicts with a different asset and cannot be overwritten automatically")
				_ = fail(err)
				return ImportExecuteResult{}, err
			}
		}
		for _, assetID := range dedupeStrings(planOverwriteTargets(plan.Conflicts)) {
			used, err := s.assetInUse(ctx, assetID)
			if err != nil {
				_ = fail(err)
				return ImportExecuteResult{}, err
			}
			if used && !in.ConfirmOverwriteInUse && !in.ConfirmDangerousOverwrite {
				err := panelerr.Validation("key_asset_import_confirmation_required", "Overwrite confirmation is required for in-use key assets")
				_ = fail(err)
				return ImportExecuteResult{}, err
			}
		}
	}
	assets := cloneStoredAssets(plan.Assets)
	skipped := 0
	idMap := map[string]string{}
	for i := range assets {
		originalID := assets[i].ID
		if strategy == "generate_new_id" {
			if _, err := s.getStoredAsset(ctx, originalID); err == nil {
				assets[i].ID = id.New(importIDPrefix(assets[i].Type))
			}
			if existingByName, err := s.getStoredAssetByName(ctx, assets[i].Name); err == nil && existingByName.ID != assets[i].ID {
				assets[i].Name = generatedImportName(assets[i].Name, assets[i].ID)
			}
		}
		idMap[originalID] = assets[i].ID
	}
	for i := range assets {
		if parentID := strings.TrimSpace(assets[i].ParentAssetID); parentID != "" {
			if mapped := idMap[parentID]; mapped != "" {
				assets[i].ParentAssetID = mapped
			}
		}
	}
	imported := []Asset{}
	err = withTx(ctx, s.db, func(tx *sql.Tx) error {
		for _, asset := range assets {
			if strategy == "skip_existing" {
				if _, err := s.getStoredAssetTx(ctx, tx, asset.ID); err == nil {
					skipped++
					continue
				}
				if existingByName, err := s.getStoredAssetByNameTx(ctx, tx, asset.Name); err == nil && existingByName.ID != asset.ID {
					skipped++
					continue
				}
			}
			if strategy == "overwrite_existing" {
				if existing, err := s.getStoredAssetTx(ctx, tx, asset.ID); err == nil {
					asset.CreatedAt = existing.CreatedAt
					asset.UpdatedAt = time.Now().UTC()
					if err := s.updateAssetTx(ctx, tx, asset); err != nil {
						return err
					}
					imported = append(imported, decorateAsset(asset.Asset, nil, 0))
					continue
				}
			}
			if asset.CreatedAt.IsZero() {
				asset.CreatedAt = time.Now().UTC()
			}
			asset.UpdatedAt = asset.CreatedAt
			if err := s.insertAssetTx(ctx, tx, asset); err != nil {
				return err
			}
			imported = append(imported, decorateAsset(asset.Asset, nil, 0))
		}
		return nil
	})
	if err != nil {
		_ = fail(err)
		return ImportExecuteResult{}, err
	}
	if err := s.refreshApplications(ctx); err != nil {
		_ = fail(err)
		return ImportExecuteResult{}, err
	}
	s.forgetImportPlan(planID)
	if err := complete("Imported key assets"); err != nil {
		return ImportExecuteResult{}, err
	}
	_ = imported
	_ = skipped
	return ImportExecuteResult{TaskID: taskID}, nil
}

func (s *Service) beginAssetTask(ctx context.Context, taskType, resourceID, summary string) (string, func(error) error, func(string) error, error) {
	if s.tasks == nil {
		return "", func(error) error { return nil }, func(string) error { return nil }, nil
	}
	task, err := s.tasks.Create(ctx, tasks.CreateInput{
		Type:         taskType,
		ResourceType: "key_asset",
		ResourceID:   resourceID,
		Status:       tasks.StatusRunning,
		Summary:      summary,
	})
	if err != nil {
		return "", nil, nil, err
	}
	taskID := task.ID
	fail := func(err error) error {
		defer s.tasks.FinishExecution(taskID)
		return s.tasks.Fail(ctx, taskID, err)
	}
	complete := func(message string) error {
		defer s.tasks.FinishExecution(taskID)
		return s.tasks.Complete(ctx, taskID, message)
	}
	return taskID, fail, complete, nil
}

func (s *Service) refreshApplications(ctx context.Context) error {
	if s.applications == nil {
		return nil
	}
	if _, err := s.applications.RedeployEnabledApplications(ctx); err != nil {
		return err
	}
	return s.applications.ReconcileReverseProxy(ctx)
}

func (s *Service) prepareImportedAsset(ctx context.Context, in ImportRequest, forcedID string) (storedAsset, error) {
	assetType := strings.TrimSpace(in.Type)
	switch assetType {
	case TypeCACertificate, TypeTLSCertificate:
		return s.prepareImportedCertificateAsset(ctx, in, forcedID)
	case TypeSSHKeyPair:
		return s.prepareImportedSSHAsset(in, forcedID)
	default:
		return storedAsset{}, panelerr.Validation("key_asset_type_invalid", "Key asset type is invalid")
	}
}

func (s *Service) prepareImportedCertificateAsset(ctx context.Context, in ImportRequest, forcedID string) (storedAsset, error) {
	cert, normalizedCertPEM, err := parseCertificatePEM(in.CertificatePEM)
	if err != nil {
		return storedAsset{}, err
	}
	key, privateKeyPEM, err := parsePrivateKeyPEM(in.PrivateKeyPEM)
	if err != nil {
		return storedAsset{}, err
	}
	if err := ensurePublicKeyMatches(cert.PublicKey, key.public); err != nil {
		return storedAsset{}, err
	}
	publicKeyText, explicitPublicKey, err := parsePublicKeyText(firstNonEmpty(in.PublicKeyPEM, in.PublicKey))
	if err != nil {
		return storedAsset{}, err
	}
	if explicitPublicKey != nil {
		if err := ensurePublicKeyMatches(key.public, explicitPublicKey); err != nil {
			return storedAsset{}, err
		}
	} else {
		publicKeyPEM, err := marshalPublicKeyPEM(key.public)
		if err != nil {
			return storedAsset{}, err
		}
		publicKeyText = string(publicKeyPEM)
	}
	assetType := strings.TrimSpace(in.Type)
	if cert.IsCA {
		assetType = TypeCACertificate
	} else if assetType == "" {
		assetType = TypeTLSCertificate
	}
	if assetType == TypeCACertificate && !cert.IsCA {
		return storedAsset{}, panelerr.Validation("key_asset_ca_invalid", "Certificate is not a CA certificate")
	}
	parentID := strings.TrimSpace(in.ParentAssetID)
	if assetType == TypeTLSCertificate && parentID != "" {
		parent, err := s.getStoredAsset(ctx, parentID)
		if err != nil {
			return storedAsset{}, err
		}
		if parent.Type != TypeCACertificate {
			return storedAsset{}, panelerr.Validation("key_asset_ca_invalid", "Selected parent asset is not a CA certificate")
		}
		parentCert, err := parent.certificate()
		if err != nil {
			return storedAsset{}, err
		}
		if err := cert.CheckSignatureFrom(parentCert); err != nil {
			return storedAsset{}, panelerr.Validation("key_asset_ca_invalid", "TLS certificate is not signed by the selected CA")
		}
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = strings.TrimSpace(cert.Subject.CommonName)
		if name == "" {
			name = forcedID
		}
	}
	commonName := strings.TrimSpace(in.CommonName)
	if commonName == "" {
		commonName = cert.Subject.CommonName
	}
	ips := make([]string, 0, len(cert.IPAddresses))
	for _, ip := range cert.IPAddresses {
		ips = append(ips, ip.String())
	}
	return storedAsset{
		Asset: Asset{
			ID:            firstNonEmpty(forcedID, id.New(importIDPrefix(assetType))),
			Type:          assetType,
			Name:          name,
			ParentAssetID: parentID,
			Algorithm:     key.algorithm,
			KeySize:       key.keySize,
			CommonName:    commonName,
			DNSNames:      append([]string(nil), cert.DNSNames...),
			IPAddresses:   ips,
			Fingerprint:   certificateFingerprint(cert),
			PublicKey:     publicKeyText,
			Metadata:      map[string]any{"origin": "imported"},
			NotBefore:     cert.NotBefore,
			NotAfter:      cert.NotAfter,
		},
		certificateText:      string(normalizedCertPEM),
		privateKeyCiphertext: string(privateKeyPEM),
	}, nil
}

func (s *Service) prepareImportedSSHAsset(in ImportRequest, forcedID string) (storedAsset, error) {
	key, privateKeyPEM, err := parsePrivateKeyPEM(in.PrivateKeyPEM)
	if err != nil {
		return storedAsset{}, err
	}
	publicKeyText, explicitPublicKey, err := parsePublicKeyText(firstNonEmpty(in.PublicKeyPEM, in.PublicKey))
	if err != nil {
		return storedAsset{}, err
	}
	if explicitPublicKey != nil {
		if err := ensurePublicKeyMatches(key.public, explicitPublicKey); err != nil {
			return storedAsset{}, err
		}
	} else {
		material, err := buildSSHMaterial(key, privateKeyPEM, "")
		if err != nil {
			return storedAsset{}, err
		}
		publicKeyText = material.publicKeyText
	}
	sshPublic, _, _, _, err := ssh.ParseAuthorizedKey([]byte(publicKeyText))
	if err != nil {
		return storedAsset{}, panelerr.Validation("key_asset_type_invalid", "SSH public key is invalid")
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = firstNonEmpty(forcedID, id.New("ssh"))
	}
	return storedAsset{
		Asset: Asset{
			ID:          firstNonEmpty(forcedID, id.New("ssh")),
			Type:        TypeSSHKeyPair,
			Name:        name,
			Algorithm:   key.algorithm,
			KeySize:     key.keySize,
			Fingerprint: sshFingerprint(sshPublic),
			PublicKey:   strings.TrimSpace(publicKeyText),
			Metadata:    map[string]any{"origin": "imported"},
		},
		privateKeyCiphertext: string(privateKeyPEM),
	}, nil
}

func (s *Service) parsePanelFileSource(ctx context.Context, source string) (string, string, error) {
	parts := strings.Split(strings.TrimSpace(source), ":")
	if len(parts) != 3 {
		return "", "", panelerr.Validation("panel_file_source_invalid", "Panel file source is invalid")
	}
	switch parts[0] {
	case "key_asset":
		return parts[1], parts[2], nil
	case "certificate":
		asset, err := s.getStoredAsset(ctx, parts[1])
		if err != nil {
			return "", "", panelerr.NotFound("Panel key asset file")
		}
		kind := parts[2]
		if asset.Type == TypeSSHKeyPair && kind == "public_key" {
			kind = "ssh_public_key"
		}
		return parts[1], kind, nil
	default:
		return "", "", panelerr.Validation("panel_file_source_invalid", "Panel file source is invalid")
	}
}

func (s *Service) getStoredAsset(ctx context.Context, assetID string) (storedAsset, error) {
	asset, err := scanStoredAsset(s.db.QueryRowContext(ctx, `SELECT `+assetColumns+` FROM key_assets WHERE id=?`, assetID))
	if err == sql.ErrNoRows {
		return storedAsset{}, panelerr.NotFound("key asset")
	}
	return asset, err
}

func (s *Service) getStoredAssetByName(ctx context.Context, name string) (storedAsset, error) {
	asset, err := scanStoredAsset(s.db.QueryRowContext(ctx, `SELECT `+assetColumns+` FROM key_assets WHERE name=?`, strings.TrimSpace(name)))
	if err == sql.ErrNoRows {
		return storedAsset{}, panelerr.NotFound("key asset")
	}
	return asset, err
}

func (s *Service) getStoredAssetTx(ctx context.Context, tx *sql.Tx, assetID string) (storedAsset, error) {
	asset, err := scanStoredAsset(tx.QueryRowContext(ctx, `SELECT `+assetColumns+` FROM key_assets WHERE id=?`, assetID))
	if err == sql.ErrNoRows {
		return storedAsset{}, panelerr.NotFound("key asset")
	}
	return asset, err
}

func (s *Service) getStoredAssetByNameTx(ctx context.Context, tx *sql.Tx, name string) (storedAsset, error) {
	asset, err := scanStoredAsset(tx.QueryRowContext(ctx, `SELECT `+assetColumns+` FROM key_assets WHERE name=?`, strings.TrimSpace(name)))
	if err == sql.ErrNoRows {
		return storedAsset{}, panelerr.NotFound("key asset")
	}
	return asset, err
}

func (s *Service) insertAsset(ctx context.Context, asset storedAsset) error {
	return withTx(ctx, s.db, func(tx *sql.Tx) error {
		return s.insertAssetTx(ctx, tx, asset)
	})
}

func (s *Service) insertAssetTx(ctx context.Context, tx *sql.Tx, asset storedAsset) error {
	privateKeyCiphertext, err := s.secrets.Encrypt(asset.ID, asset.Type, "private_key", []byte(asset.privateKeyCiphertext))
	if err != nil {
		return err
	}
	metadataJSON, err := json.Marshal(asset.Metadata)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO key_assets(id,type,name,parent_asset_id,algorithm,key_size,common_name,dns_names_json,ip_addresses_json,fingerprint,certificate_ciphertext,private_key_ciphertext,public_key,metadata_json,not_before,not_after,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		asset.ID, asset.Type, asset.Name, strings.TrimSpace(asset.ParentAssetID), asset.Algorithm, asset.KeySize, asset.CommonName, mustJSON(asset.DNSNames), mustJSON(asset.IPAddresses), asset.Fingerprint, asset.certificateText, privateKeyCiphertext, asset.PublicKey, string(metadataJSON), formatOptionalTime(asset.NotBefore), formatOptionalTime(asset.NotAfter), formatTime(asset.CreatedAt), formatTime(asset.UpdatedAt))
	return err
}

func (s *Service) updateAsset(ctx context.Context, asset storedAsset) error {
	return withTx(ctx, s.db, func(tx *sql.Tx) error {
		return s.updateAssetTx(ctx, tx, asset)
	})
}

func (s *Service) updateAssetTx(ctx context.Context, tx *sql.Tx, asset storedAsset) error {
	privateKeyCiphertext, err := s.secrets.Encrypt(asset.ID, asset.Type, "private_key", []byte(asset.privateKeyCiphertext))
	if err != nil {
		return err
	}
	metadataJSON, err := json.Marshal(asset.Metadata)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE key_assets SET type=?,name=?,parent_asset_id=?,algorithm=?,key_size=?,common_name=?,dns_names_json=?,ip_addresses_json=?,fingerprint=?,certificate_ciphertext=?,private_key_ciphertext=?,public_key=?,metadata_json=?,not_before=?,not_after=?,updated_at=? WHERE id=?`,
		asset.Type, asset.Name, strings.TrimSpace(asset.ParentAssetID), asset.Algorithm, asset.KeySize, asset.CommonName, mustJSON(asset.DNSNames), mustJSON(asset.IPAddresses), asset.Fingerprint, asset.certificateText, privateKeyCiphertext, asset.PublicKey, string(metadataJSON), formatOptionalTime(asset.NotBefore), formatOptionalTime(asset.NotAfter), formatTime(asset.UpdatedAt), asset.ID)
	return err
}

func (s *Service) decryptPrivateKeyPEM(asset storedAsset) ([]byte, error) {
	return s.secrets.Decrypt(asset.ID, asset.Type, "private_key", asset.privateKeyCiphertext)
}

func (s *Service) decryptPrivateKey(asset storedAsset) (crypto.PrivateKey, error) {
	privateKeyPEM, err := s.decryptPrivateKeyPEM(asset)
	if err != nil {
		return nil, err
	}
	key, _, err := parsePrivateKeyPEM(string(privateKeyPEM))
	if err != nil {
		return nil, err
	}
	return key.private, nil
}

func (asset storedAsset) certificate() (*x509.Certificate, error) {
	if strings.TrimSpace(asset.certificateText) == "" {
		return nil, panelerr.Validation("key_asset_type_invalid", "Certificate asset has no certificate data")
	}
	cert, _, err := parseCertificatePEM(asset.certificateText)
	return cert, err
}

func (s *Service) assetReferences(ctx context.Context) (map[string][]AssetReference, error) {
	tlsAssets, err := s.listStoredAssetsByType(ctx, TypeTLSCertificate)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,spec_yaml,reverse_proxy_json FROM applications`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	references := map[string][]AssetReference{}
	for rows.Next() {
		var applicationID, applicationName, specYAML, reverseProxyJSON string
		if err := rows.Scan(&applicationID, &applicationName, &specYAML, &reverseProxyJSON); err != nil {
			return nil, err
		}
		for _, prefix := range []string{"key_asset:", "certificate:"} {
			for _, assetID := range extractAssetIDsFromSpec(specYAML, prefix) {
				references[assetID] = appendAssetReference(references[assetID], AssetReference{
					ResourceType: "application",
					ResourceID:   applicationID,
					ResourceName: applicationName,
					Relation:     "panel_file",
				})
			}
		}
		var rules []struct {
			Domain string `json:"domain"`
		}
		_ = json.Unmarshal([]byte(reverseProxyJSON), &rules)
		if len(rules) == 0 {
			continue
		}
		for _, asset := range tlsAssets {
			for _, rule := range rules {
				domains := append(append([]string(nil), asset.DNSNames...), asset.IPAddresses...)
				for _, domain := range domains {
					if certificateDomainMatches(domain, rule.Domain) {
						references[asset.ID] = appendAssetReference(references[asset.ID], AssetReference{
							ResourceType: "application",
							ResourceID:   applicationID,
							ResourceName: applicationName,
							Relation:     "reverse_proxy",
						})
					}
				}
			}
		}
	}
	return references, rows.Err()
}

func (s *Service) assetInUse(ctx context.Context, assetID string) (bool, error) {
	references, err := s.assetReferences(ctx)
	if err != nil {
		return false, err
	}
	return len(references[assetID]) > 0, nil
}

func (s *Service) listStoredAssetsByType(ctx context.Context, assetType string) ([]storedAsset, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+assetColumns+` FROM key_assets WHERE type=? ORDER BY name`, assetType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	assets := []storedAsset{}
	for rows.Next() {
		asset, err := scanStoredAsset(rows)
		if err != nil {
			return nil, err
		}
		assets = append(assets, asset)
	}
	return assets, rows.Err()
}

func appendAssetReference(references []AssetReference, candidate AssetReference) []AssetReference {
	for _, reference := range references {
		if reference.ResourceType == candidate.ResourceType &&
			reference.ResourceID == candidate.ResourceID &&
			reference.Relation == candidate.Relation {
			return references
		}
	}
	return append(references, candidate)
}

func (s *Service) childCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT parent_asset_id,COUNT(*) FROM key_assets WHERE parent_asset_id<>'' GROUP BY parent_asset_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var parentID string
		var count int
		if err := rows.Scan(&parentID, &count); err != nil {
			return nil, err
		}
		counts[parentID] = count
	}
	return counts, rows.Err()
}

func scanStoredAsset(row interface{ Scan(dest ...any) error }) (storedAsset, error) {
	var asset storedAsset
	var dnsNamesJSON, ipAddressesJSON, metadataJSON string
	var notBefore, notAfter, createdAt, updatedAt string
	err := row.Scan(&asset.ID, &asset.Type, &asset.Name, &asset.ParentAssetID, &asset.Algorithm, &asset.KeySize, &asset.CommonName, &dnsNamesJSON, &ipAddressesJSON, &asset.Fingerprint, &asset.certificateText, &asset.privateKeyCiphertext, &asset.PublicKey, &metadataJSON, &notBefore, &notAfter, &createdAt, &updatedAt)
	if err != nil {
		return storedAsset{}, err
	}
	_ = json.Unmarshal([]byte(dnsNamesJSON), &asset.DNSNames)
	_ = json.Unmarshal([]byte(ipAddressesJSON), &asset.IPAddresses)
	if metadataJSON != "" {
		_ = json.Unmarshal([]byte(metadataJSON), &asset.Metadata)
	}
	if asset.Metadata == nil {
		asset.Metadata = map[string]any{}
	}
	asset.NotBefore = parseTime(notBefore)
	asset.NotAfter = parseTime(notAfter)
	asset.CreatedAt = parseTime(createdAt)
	asset.UpdatedAt = parseTime(updatedAt)
	return asset, nil
}

const assetColumns = `id,type,name,parent_asset_id,algorithm,key_size,common_name,dns_names_json,ip_addresses_json,fingerprint,certificate_ciphertext,private_key_ciphertext,public_key,metadata_json,not_before,not_after,created_at,updated_at`

func decorateAsset(asset Asset, references []AssetReference, childCount int) Asset {
	asset.References = append([]AssetReference(nil), references...)
	asset.InUse = len(references) > 0
	asset.ChildCount = childCount
	asset.HasChildren = childCount > 0
	asset.CanReissue = asset.Type == TypeTLSCertificate && strings.TrimSpace(asset.ParentAssetID) != ""
	asset.CanRegenerate = asset.Type == TypeSSHKeyPair
	asset.FileKinds = fileKindsForAsset(asset.Type)
	return asset
}

func importIDPrefix(assetType string) string {
	switch assetType {
	case TypeCACertificate:
		return "ca"
	case TypeTLSCertificate:
		return "cert"
	default:
		return "ssh"
	}
}

func withTx(ctx context.Context, db *sql.DB, fn func(*sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func mustJSON(value any) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}

func parseTime(value string) time.Time {
	if strings.TrimSpace(value) == "" {
		return time.Time{}
	}
	parsed, _ := time.Parse(time.RFC3339Nano, value)
	return parsed
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

func nullString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.TrimSpace(value)
}

func cloneMetadata(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	raw, _ := json.Marshal(value)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return out
}

func cloneStoredAssets(values []storedAsset) []storedAsset {
	out := make([]storedAsset, 0, len(values))
	for _, value := range values {
		next := value
		next.DNSNames = append([]string(nil), value.DNSNames...)
		next.IPAddresses = append([]string(nil), value.IPAddresses...)
		next.Metadata = cloneMetadata(value.Metadata)
		out = append(out, next)
	}
	return out
}

func dedupeStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func dedupeConflicts(values []ImportConflict) []ImportConflict {
	type key struct {
		IncomingID string
		ExistingID string
		ByID       bool
		ByName     bool
	}
	seen := map[key]struct{}{}
	out := make([]ImportConflict, 0, len(values))
	for _, value := range values {
		k := key{IncomingID: value.IncomingID, ExistingID: value.ExistingID, ByID: value.ConflictByID, ByName: value.ConflictByName}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, value)
	}
	return out
}

func generatedImportName(name, assetID string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return assetID
	}
	suffix := assetID
	if len(suffix) > 8 {
		suffix = suffix[:8]
	}
	return name + "-" + suffix
}

func planOverwriteTargets(conflicts []ImportConflict) []string {
	out := []string{}
	for _, conflict := range conflicts {
		if conflict.ExistingID != "" {
			out = append(out, conflict.ExistingID)
		}
	}
	return out
}

func (s *Service) getImportPlan(planID string) (*importPlan, error) {
	s.planMu.Lock()
	defer s.planMu.Unlock()
	plan := s.importPlans[planID]
	if plan == nil || time.Now().UTC().After(plan.ExpiresAt) {
		if plan != nil {
			_ = os.Remove(plan.FilePath)
			delete(s.importPlans, planID)
		}
		return nil, panelerr.Validation("key_asset_import_plan_expired", "Key asset import plan has expired")
	}
	return plan, nil
}

func (s *Service) forgetImportPlan(planID string) {
	s.planMu.Lock()
	plan := s.importPlans[planID]
	delete(s.importPlans, planID)
	s.planMu.Unlock()
	if plan != nil {
		_ = os.Remove(plan.FilePath)
	}
}

func extractAssetIDsFromSpec(specYAML, prefix string) []string {
	out := []string{}
	for _, line := range strings.Split(specYAML, "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, prefix) {
			continue
		}
		idx := strings.Index(line, prefix)
		if idx < 0 {
			continue
		}
		value := line[idx:]
		value = strings.TrimPrefix(value, prefix)
		parts := strings.Split(value, ":")
		if len(parts) < 2 {
			continue
		}
		out = append(out, strings.Trim(parts[0], `"'`))
	}
	return out
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
