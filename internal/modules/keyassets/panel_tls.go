package keyassets

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"net"
	"strings"
	"time"

	serverdomain "panel/internal/modules/servers/domain"
	"panel/internal/modules/tasks"
	"panel/internal/platform/database/orm"
	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/paneltls"
)

const (
	// Stable IDs let startup repair the built-in chain instead of creating a
	// new system asset on every restart.
	SystemPanelCAAssetID  = "panel-ca"
	SystemPanelTLSAssetID = "panel-tls"
	systemPanelScope      = "panel_tls"
	panelTLSRenewBefore   = 30 * 24 * time.Hour
	panelTLSValidity      = 365 * 24 * time.Hour
	panelCAValidity       = 30 * 365 * 24 * time.Hour
)

// EnsurePanelTLSAssets ensures the built-in Panel RSA CA and leaf exist in
// key_assets. The returned leaf is not activated until SyncPanelTLS runs.
func (s *Service) EnsurePanelTLSAssets(ctx context.Context, domain string) (Asset, error) {
	domain = normalizePanelDomain(domain)
	ca, err := s.getStoredAsset(ctx, SystemPanelCAAssetID)
	if err != nil && !isNotFoundError(err) {
		return Asset{}, err
	}
	if err != nil || !s.panelCAValid(ca) {
		ca, err = s.newPanelCAAsset(SystemPanelCAAssetID)
		if err != nil {
			return Asset{}, err
		}
		var leaf storedAsset
		if err := orm.WithTx(ctx, s.db, func(tx *sql.Tx) error {
			if err := s.deletePanelSystemAssetsTx(ctx, tx); err != nil {
				return err
			}
			if err := s.insertAssetTx(ctx, tx, ca); err != nil {
				return err
			}
			storedCA, err := s.getStoredAssetTx(ctx, tx, SystemPanelCAAssetID)
			if err != nil {
				return err
			}
			leaf, err = s.newPanelLeafAsset(SystemPanelTLSAssetID, storedCA, domain)
			if err != nil {
				return err
			}
			return s.insertAssetTx(ctx, tx, leaf)
		}); err != nil {
			return Asset{}, err
		}
		return leaf.Asset, nil
	}

	leaf, leafErr := s.getStoredAsset(ctx, SystemPanelTLSAssetID)
	if leafErr != nil && !isNotFoundError(leafErr) {
		return Asset{}, leafErr
	}
	if leafErr != nil || !s.validPanelLeaf(leaf, ca, domain) || time.Until(leaf.NotAfter) <= panelTLSRenewBefore {
		leaf, err = s.newPanelLeafAsset(SystemPanelTLSAssetID, ca, domain)
		if err != nil {
			return Asset{}, err
		}
		if err := s.upsertPanelAsset(ctx, leaf); err != nil {
			return Asset{}, err
		}
	}
	return leaf.Asset, nil
}

// SyncPanelTLS activates the selected asset. An empty selection always uses
// the built-in RSA leaf after ensuring the system chain exists.
func (s *Service) syncPanelTLS(ctx context.Context, domain, assetID string) error {
	// The managed chain remains healthy even while an administrator has chosen
	// a custom listener certificate. This keeps the default recovery path ready
	// without changing that selection.
	if _, err := s.EnsurePanelTLSAssets(ctx, domain); err != nil {
		return err
	}
	assetID = strings.TrimSpace(assetID)
	if assetID == "" {
		assetID = SystemPanelTLSAssetID
	}
	asset, err := s.getStoredAsset(ctx, assetID)
	if err != nil {
		return err
	}
	return s.activatePanelTLSAsset(ctx, domain, asset)
}

// PanelSystemCertificates exposes the built-in chain to the system
// certificate view without exposing it as a user-managed asset.
func (s *Service) PanelSystemCertificates(ctx context.Context) ([]Asset, error) {
	domain, _, err := s.panelTLSSelection(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := s.EnsurePanelTLSAssets(ctx, domain); err != nil {
		return nil, err
	}
	ca, err := s.getStoredAsset(ctx, SystemPanelCAAssetID)
	if err != nil {
		return nil, err
	}
	leaf, err := s.getStoredAsset(ctx, SystemPanelTLSAssetID)
	if err != nil {
		return nil, err
	}
	return []Asset{ca.Asset, leaf.Asset}, nil
}

// PanelSystemCertificateInfos adapts the built-in assets to the existing
// settings system-certificate contract. The underlying material remains in
// key_assets and is never exposed through the user asset list.
func (s *Service) PanelSystemCertificateInfos(ctx context.Context) ([]serverdomain.SystemCertificate, error) {
	assets, err := s.PanelSystemCertificates(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]serverdomain.SystemCertificate, 0, len(assets))
	for _, asset := range assets {
		notBefore, notAfter := asset.NotBefore, asset.NotAfter
		result = append(result, serverdomain.SystemCertificate{
			ID:          asset.ID,
			Type:        asset.Type,
			Name:        asset.Name,
			CommonName:  asset.CommonName,
			Fingerprint: asset.Fingerprint,
			NotBefore:   &notBefore,
			NotAfter:    &notAfter,
			Status:      panelCertificateTimeStatus(asset.NotBefore, asset.NotAfter, time.Now()),
			BuiltIn:     true,
			CanReset:    true,
		})
	}
	return result, nil
}

func panelCertificateTimeStatus(notBefore, notAfter, now time.Time) string {
	if now.Before(notBefore) {
		return "not_yet_valid"
	}
	if now.After(notAfter) {
		return "expired"
	}
	return "valid"
}

func listenerCertificateSignatureAlgorithm(algorithm x509.SignatureAlgorithm) bool {
	switch algorithm {
	case x509.SHA256WithRSA, x509.SHA384WithRSA, x509.SHA512WithRSA:
		return true
	default:
		return false
	}
}

func (s *Service) activatePanelTLSAsset(ctx context.Context, domain string, asset storedAsset) error {
	if asset.Type != TypeTLSCertificate {
		return panelerr.Validation("invalid_panel_tls_certificate", "Selected Panel certificate must be a TLS certificate")
	}
	certificatePEM, err := s.panelTLSCertificateChain(ctx, asset)
	if err != nil {
		return err
	}
	privateKeyPEM, err := s.panelTLSPrivateKeyPEM(asset)
	if err != nil {
		return err
	}
	certificate, err := tls.X509KeyPair(certificatePEM, privateKeyPEM)
	if err != nil || len(certificate.Certificate) == 0 {
		return panelerr.Validation("invalid_panel_tls_certificate", "Selected Panel TLS certificate and private key do not match")
	}
	if err := paneltls.ValidateListenerCertificate(certificate, normalizePanelDomain(domain)); err != nil {
		return panelerr.Validation("invalid_panel_tls_certificate", err.Error())
	}
	return paneltls.SyncCertificate(ctx, s.cfg.DataRoot, domain, asset.ID, panelTLSMaterialReader{
		certificatePEM: certificatePEM,
		privateKeyPEM:  privateKeyPEM,
	})
}

func (s *Service) panelTLSCertificateChain(ctx context.Context, asset storedAsset) ([]byte, error) {
	certificatePEM := []byte(asset.certificateText)
	current, err := asset.certificate()
	if err != nil {
		return nil, err
	}
	visited := map[string]struct{}{asset.ID: {}}
	parentID := strings.TrimSpace(asset.ParentAssetID)
	for parentID != "" {
		if _, ok := visited[parentID]; ok {
			return nil, panelerr.Validation("invalid_panel_tls_certificate", "Selected Panel TLS certificate chain contains a cycle")
		}
		visited[parentID] = struct{}{}
		parent, err := s.getStoredAsset(ctx, parentID)
		if err != nil {
			return nil, err
		}
		if parent.Type != TypeCACertificate {
			return nil, panelerr.Validation("invalid_panel_tls_certificate", "Selected Panel certificate parent is not a CA certificate")
		}
		parentCert, err := parent.certificate()
		if err != nil || !parentCert.IsCA || current.CheckSignatureFrom(parentCert) != nil {
			return nil, panelerr.Validation("invalid_panel_tls_certificate", "Selected Panel TLS certificate is not signed by its parent CA")
		}
		certificatePEM = append(certificatePEM, []byte(parent.certificateText)...)
		current = parentCert
		parentID = strings.TrimSpace(parent.ParentAssetID)
	}
	return certificatePEM, nil
}

// Assets read from the database contain encrypted private material. During a
// reissue/import transaction the candidate asset has not been stored yet, so
// it still carries PEM. Accept that narrow in-memory form as well.
func (s *Service) panelTLSPrivateKeyPEM(asset storedAsset) ([]byte, error) {
	privateKeyPEM, decryptErr := s.decryptPrivateKeyPEM(asset)
	if decryptErr == nil {
		return privateKeyPEM, nil
	}
	if _, _, err := parsePrivateKeyPEM(asset.privateKeyCiphertext); err == nil {
		return []byte(asset.privateKeyCiphertext), nil
	}
	return nil, decryptErr
}

func panelCARecordValid(asset storedAsset) bool {
	if asset.ID != SystemPanelCAAssetID || asset.Type != TypeCACertificate || asset.Algorithm != AlgorithmRSA || asset.Metadata[systemManagedKey] != true || asset.Metadata[systemScopeKey] != systemPanelScope || asset.Metadata[systemRoleKey] != "ca" {
		return false
	}
	cert, err := asset.certificate()
	if err != nil || !cert.IsCA || time.Now().Before(cert.NotBefore) || time.Until(cert.NotAfter) <= panelTLSRenewBefore || cert.CheckSignatureFrom(cert) != nil {
		return false
	}
	key, ok := cert.PublicKey.(*rsa.PublicKey)
	return ok && key.N.BitLen() >= 2048 && listenerCertificateSignatureAlgorithm(cert.SignatureAlgorithm)
}

func (s *Service) panelCAValid(asset storedAsset) bool {
	if !panelCARecordValid(asset) {
		return false
	}
	keyPEM, err := s.decryptPrivateKeyPEM(asset)
	if err != nil {
		return false
	}
	_, err = tls.X509KeyPair([]byte(asset.certificateText), keyPEM)
	return err == nil
}

func (s *Service) validPanelLeaf(leaf, ca storedAsset, domain string) bool {
	if leaf.ID != SystemPanelTLSAssetID || leaf.Type != TypeTLSCertificate || leaf.Algorithm != AlgorithmRSA || leaf.Metadata[systemManagedKey] != true || leaf.Metadata[systemScopeKey] != systemPanelScope || leaf.Metadata[systemRoleKey] != "tls" || time.Now().Before(leaf.NotBefore) || time.Now().After(leaf.NotAfter) {
		return false
	}
	cert, err := leaf.certificate()
	if err != nil || cert.IsCA {
		return false
	}
	if _, ok := cert.PublicKey.(*rsa.PublicKey); !ok || strings.TrimSpace(leaf.ParentAssetID) != ca.ID {
		return false
	}
	parent, err := ca.certificate()
	if err != nil || cert.CheckSignatureFrom(parent) != nil {
		return false
	}
	keyPEM, err := s.decryptPrivateKeyPEM(leaf)
	if err != nil {
		return false
	}
	pair, err := tls.X509KeyPair(append([]byte(leaf.certificateText), []byte(ca.certificateText)...), keyPEM)
	if err != nil {
		return false
	}
	return paneltls.ValidateListenerCertificate(pair, normalizePanelDomain(domain)) == nil
}

func normalizePanelDomain(domain string) string {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return "localhost"
	}
	return domain
}

func systemMetadataForScope(scope, role string) map[string]any {
	return map[string]any{
		systemManagedKey: true,
		systemScopeKey:   scope,
		systemRoleKey:    role,
		"origin":         "system",
	}
}

func (s *Service) newPanelCAAsset(assetID string) (storedAsset, error) {
	key, err := generateKeyMaterial(AlgorithmRSA, 2048)
	if err != nil {
		return storedAsset{}, err
	}
	privateKeyPEM, err := marshalPrivateKeyPEM(key.private)
	if err != nil {
		return storedAsset{}, err
	}
	now := time.Now().UTC()
	template, err := newCertificateTemplate("Panel HTTPS CA", now.Add(-time.Hour), now.Add(panelCAValidity), true, nil, nil)
	if err != nil {
		return storedAsset{}, err
	}
	certificatePEM, err := generateCertificate(template, template, key.public, key.private)
	if err != nil {
		return storedAsset{}, err
	}
	cert, normalizedCertPEM, err := parseCertificatePEM(string(certificatePEM))
	if err != nil {
		return storedAsset{}, err
	}
	material, err := buildCertificateMaterial(cert, normalizedCertPEM, key, privateKeyPEM)
	if err != nil {
		return storedAsset{}, err
	}
	return storedAsset{Asset: Asset{
		ID: assetID, Type: TypeCACertificate, Name: "Panel HTTPS CA", Algorithm: key.algorithm,
		KeySize: key.keySize, CommonName: "Panel HTTPS CA", Fingerprint: certificateFingerprint(cert),
		PublicKey: publicKeyDisplayForCertificate(material.publicKeyPEM), Metadata: systemMetadataForScope(systemPanelScope, "ca"),
		NotBefore: cert.NotBefore, NotAfter: cert.NotAfter, CreatedAt: now, UpdatedAt: now,
	}, certificateText: string(material.certificatePEM), privateKeyCiphertext: string(material.privateKeyPEM)}, nil
}

func (s *Service) newPanelLeafAsset(assetID string, ca storedAsset, domain string) (storedAsset, error) {
	key, err := generateKeyMaterial(AlgorithmRSA, 2048)
	if err != nil {
		return storedAsset{}, err
	}
	privateKeyPEM, err := marshalPrivateKeyPEM(key.private)
	if err != nil {
		return storedAsset{}, err
	}
	parentCert, err := ca.certificate()
	if err != nil {
		return storedAsset{}, err
	}
	parentPrivateKey, err := s.panelPrivateKey(ca)
	if err != nil {
		return storedAsset{}, err
	}
	dnsNames, ips := []string{}, []net.IP{}
	if ip := net.ParseIP(domain); ip != nil {
		ips = append(ips, ip)
	} else {
		dnsNames = append(dnsNames, domain)
		if domain == "localhost" {
			ips = append(ips, net.ParseIP("127.0.0.1"), net.ParseIP("::1"))
		}
	}
	now := time.Now().UTC()
	template, err := newCertificateTemplate(domain, now.Add(-time.Hour), now.Add(panelTLSValidity), false, dnsNames, ips)
	if err != nil {
		return storedAsset{}, err
	}
	template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
	certificatePEM, err := generateCertificate(template, parentCert, key.public, parentPrivateKey)
	if err != nil {
		return storedAsset{}, err
	}
	cert, normalizedCertPEM, err := parseCertificatePEM(string(certificatePEM))
	if err != nil {
		return storedAsset{}, err
	}
	material, err := buildCertificateMaterial(cert, normalizedCertPEM, key, privateKeyPEM)
	if err != nil {
		return storedAsset{}, err
	}
	ipStrings := make([]string, 0, len(ips))
	for _, ip := range ips {
		ipStrings = append(ipStrings, ip.String())
	}
	return storedAsset{Asset: Asset{
		ID: assetID, Type: TypeTLSCertificate, Name: "Panel HTTPS certificate", ParentAssetID: ca.ID,
		Algorithm: key.algorithm, KeySize: key.keySize, CommonName: domain, DNSNames: dnsNames,
		IPAddresses: ipStrings, Fingerprint: certificateFingerprint(cert), PublicKey: publicKeyDisplayForCertificate(material.publicKeyPEM),
		Metadata: systemMetadataForScope(systemPanelScope, "tls"), NotBefore: cert.NotBefore, NotAfter: cert.NotAfter,
		CreatedAt: now, UpdatedAt: now,
	}, certificateText: string(material.certificatePEM), privateKeyCiphertext: string(material.privateKeyPEM)}, nil
}

func (s *Service) upsertPanelAsset(ctx context.Context, asset storedAsset) error {
	return orm.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if existing, err := s.getStoredAssetTx(ctx, tx, asset.ID); err == nil {
			asset.CreatedAt = existing.CreatedAt
			return s.updateAssetTx(ctx, tx, asset)
		} else if !isNotFoundError(err) {
			return err
		}
		return s.insertAssetTx(ctx, tx, asset)
	})
}

func (s *Service) deletePanelSystemAssets(ctx context.Context) error {
	return orm.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		return s.deletePanelSystemAssetsTx(ctx, tx)
	})
}

func (s *Service) deletePanelSystemAssetsTx(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM key_assets WHERE id IN (?,?)`, SystemPanelCAAssetID, SystemPanelTLSAssetID)
	return err
}

func (s *Service) panelPrivateKey(asset storedAsset) (crypto.PrivateKey, error) {
	keyPEM, err := s.decryptPrivateKeyPEM(asset)
	if err != nil {
		if _, _, parseErr := parsePrivateKeyPEM(asset.privateKeyCiphertext); parseErr == nil {
			keyPEM = []byte(asset.privateKeyCiphertext)
		} else {
			return nil, err
		}
	}
	key, _, err := parsePrivateKeyPEM(string(keyPEM))
	if err != nil {
		return nil, err
	}
	return key.private, nil
}

// ResetPanelTLS queues a user-visible reset through the same task worker used
// for scheduled reconciliation. Resetting a system asset never changes a
// custom certificate selected in runtime settings.
func (s *Service) ResetPanelTLS(ctx context.Context, assetID string) (tasks.Task, error) {
	assetID = strings.TrimSpace(assetID)
	if assetID != SystemPanelCAAssetID && assetID != SystemPanelTLSAssetID {
		return tasks.Task{}, panelerr.NotFound("system certificate")
	}
	if s.tasks == nil {
		return tasks.Task{}, panelerr.Validation("task_service_unavailable", "Task service is unavailable")
	}
	manager := tasks.NewManager(s.tasks)
	task, created, err := manager.Create(ctx, tasks.CreateInput{
		Type:         TaskTypePanelTLSReconcile,
		ResourceType: "key_asset",
		ResourceID:   assetID,
		TriggeredBy:  "user",
		Summary:      "Resetting system-managed Panel TLS certificate",
		Status:       tasks.StatusRunning,
	}, tasks.Trigger{Type: "user", Manual: true})
	if err != nil || !created {
		return task, err
	}
	go s.runPanelTLSReset(context.Background(), task.ID, assetID)
	return task, nil
}

func (s *Service) runPanelTLSReset(ctx context.Context, taskID, assetID string) {
	defer s.tasks.FinishExecution(taskID)
	_ = s.tasks.Advance(ctx, taskID, "regenerating", "regenerating system-managed Panel TLS certificate")
	err := paneltls.WithUpdate(func() error {
		if assetID == SystemPanelCAAssetID {
			if err := s.deletePanelSystemAssets(ctx); err != nil {
				return err
			}
		} else if err := s.deleteStoredAssetIfExists(ctx, SystemPanelTLSAssetID); err != nil {
			return err
		}
		domain, selected, err := s.panelTLSSelection(ctx)
		if err != nil {
			return err
		}
		if _, err := s.EnsurePanelTLSAssets(ctx, domain); err != nil {
			return err
		}
		if strings.TrimSpace(selected) == "" || selected == SystemPanelTLSAssetID {
			asset, err := s.getStoredAsset(ctx, SystemPanelTLSAssetID)
			if err != nil {
				return err
			}
			return s.activatePanelTLSAsset(ctx, domain, asset)
		}
		return nil
	})
	if err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	_ = s.tasks.Complete(ctx, taskID, "System-managed Panel TLS certificate reset")
}

// RunPanelTLSReconcileTask is the executor used by the existing task worker.
func (s *Service) RunPanelTLSReconcileTask(tc tasks.TaskContext) error {
	domain, _, err := s.panelTLSSelection(tc.Context)
	if err != nil {
		return err
	}
	return paneltls.WithUpdate(func() error {
		if _, err := s.EnsurePanelTLSAssets(tc.Context, domain); err != nil {
			return err
		}
		selectedDomain, selected, err := s.panelTLSSelection(tc.Context)
		if err != nil {
			return err
		}
		if strings.TrimSpace(selected) == "" {
			asset, err := s.getStoredAsset(tc.Context, SystemPanelTLSAssetID)
			if err != nil {
				return err
			}
			return s.activatePanelTLSAsset(tc.Context, selectedDomain, asset)
		}
		return s.SyncPanelTLS(tc.Context, selectedDomain, selected)
	})
}

func (s *Service) CollectPanelTLSInputs(ctx context.Context, _ tasks.PeriodicTrigger) (tasks.CreateBatchInput, bool, error) {
	domain, _, err := s.panelTLSSelection(ctx)
	if err != nil {
		return tasks.CreateBatchInput{}, false, err
	}
	ca, caErr := s.getStoredAsset(ctx, SystemPanelCAAssetID)
	leaf, leafErr := s.getStoredAsset(ctx, SystemPanelTLSAssetID)
	needs := caErr != nil || leafErr != nil || !s.panelCAValid(ca) || !s.validPanelLeaf(leaf, ca, domain) || time.Until(leaf.NotAfter) <= panelTLSRenewBefore
	if !needs {
		return tasks.CreateBatchInput{}, false, nil
	}
	return tasks.CreateBatchInput{Type: TaskTypePanelTLSReconcile, Summary: "Reconciling Panel TLS assets", Inputs: []tasks.CreateInput{{
		Type: TaskTypePanelTLSReconcile, ResourceType: "key_asset", ResourceID: SystemPanelTLSAssetID,
		Summary: "Reconciling Panel TLS assets for " + normalizePanelDomain(domain),
	}}}, true, nil
}
