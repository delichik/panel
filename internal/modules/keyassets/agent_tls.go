package keyassets

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	agentsecurity "panel/internal/agent/security"
	panelerr "panel/internal/platform/errors"
)

const (
	SystemAgentCAAssetID     = "agent-ca"
	SystemAgentClientAssetID = "agent-panel-client"

	systemAgentScope  = "agent_tls"
	systemManagedKey  = "systemManaged"
	systemScopeKey    = "systemScope"
	systemRoleKey     = "systemRole"
	systemServerIDKey = "serverID"
)

func (s *Service) EnsureAgentTLSAssets(ctx context.Context) (*agentsecurity.TLSAssets, error) {
	ca, caErr := s.getStoredAsset(ctx, SystemAgentCAAssetID)
	client, clientErr := s.getStoredAsset(ctx, SystemAgentClientAssetID)
	if caErr == nil && clientErr == nil {
		return s.agentTLSAssetsFromStored(ca, client)
	}
	if caErr == nil && isNotFoundError(clientErr) {
		if _, err := s.createAgentClientCertificate(ctx, ca); err != nil {
			return nil, err
		}
		client, err := s.getStoredAsset(ctx, SystemAgentClientAssetID)
		if err != nil {
			return nil, err
		}
		return s.agentTLSAssetsFromStored(ca, client)
	}
	if isNotFoundError(caErr) {
		if err := s.importLegacyAgentTLSFiles(ctx); err != nil {
			return nil, err
		}
		ca, caErr = s.getStoredAsset(ctx, SystemAgentCAAssetID)
		client, clientErr = s.getStoredAsset(ctx, SystemAgentClientAssetID)
		if caErr == nil && clientErr == nil {
			return s.agentTLSAssetsFromStored(ca, client)
		}
		if err := s.resetAgentTLSAssets(ctx); err != nil {
			return nil, err
		}
		return s.EnsureAgentTLSAssets(ctx)
	}
	return nil, firstNonNil(caErr, clientErr)
}

func (s *Service) IssueAgentServerCertificate(ctx context.Context, serverID, serverName, host string) (agentsecurity.ServerCertificate, []byte, error) {
	assets, err := s.EnsureAgentTLSAssets(ctx)
	if err != nil {
		return agentsecurity.ServerCertificate{}, nil, err
	}
	cert, err := assets.IssueServerCertificate("panel-agent-"+strings.TrimSpace(serverID), []string{host})
	if err != nil {
		return agentsecurity.ServerCertificate{}, nil, err
	}
	if err := s.upsertAgentServerCertificate(ctx, serverID, serverName, cert); err != nil {
		return agentsecurity.ServerCertificate{}, nil, err
	}
	return cert, assets.CACertificatePEM(), nil
}

func (s *Service) ResetAgentCA(ctx context.Context) (*agentsecurity.TLSAssets, error) {
	if err := s.resetAgentTLSAssets(ctx); err != nil {
		return nil, err
	}
	return s.EnsureAgentTLSAssets(ctx)
}

func (s *Service) ResetAgentClientCertificate(ctx context.Context) (*agentsecurity.TLSAssets, error) {
	ca, err := s.getStoredAsset(ctx, SystemAgentCAAssetID)
	if err != nil {
		return nil, err
	}
	if err := s.deleteStoredAssetIfExists(ctx, SystemAgentClientAssetID); err != nil {
		return nil, err
	}
	if _, err := s.createAgentClientCertificate(ctx, ca); err != nil {
		return nil, err
	}
	return s.EnsureAgentTLSAssets(ctx)
}

func (s *Service) resetAgentTLSAssets(ctx context.Context) error {
	if err := s.deleteAgentSystemAssets(ctx); err != nil {
		return err
	}
	ca, err := s.newAgentCAAsset(SystemAgentCAAssetID)
	if err != nil {
		return err
	}
	if err := s.insertAsset(ctx, ca); err != nil {
		return err
	}
	storedCA, err := s.getStoredAsset(ctx, SystemAgentCAAssetID)
	if err != nil {
		return err
	}
	_, err = s.createAgentClientCertificate(ctx, storedCA)
	return err
}

func (s *Service) createAgentClientCertificate(ctx context.Context, ca storedAsset) (Asset, error) {
	asset, err := s.newAgentLeafAsset(SystemAgentClientAssetID, "Panel Agent client", ca, "client", "panel-agent-client", []string{"panel-agent-client"}, nil)
	if err != nil {
		return Asset{}, err
	}
	if err := s.insertAsset(ctx, asset); err != nil {
		return Asset{}, err
	}
	return s.Get(ctx, SystemAgentClientAssetID)
}

func (s *Service) upsertAgentServerCertificate(ctx context.Context, serverID, serverName string, cert agentsecurity.ServerCertificate) error {
	ca, err := s.getStoredAsset(ctx, SystemAgentCAAssetID)
	if err != nil {
		return err
	}
	assetID := agentServerAssetID(serverID)
	certAsset, err := s.prepareImportedAsset(ctx, ImportRequest{Type: TypeTLSCertificate, Name: "Agent server certificate - " + strings.TrimSpace(serverName), ParentAssetID: ca.ID, CertificatePEM: string(cert.CertPEM), PrivateKeyPEM: string(cert.KeyPEM)}, assetID)
	if err != nil {
		return err
	}
	certAsset.ParentAssetID = ca.ID
	certAsset.Metadata = systemMetadata("server")
	certAsset.Metadata[systemServerIDKey] = strings.TrimSpace(serverID)
	certAsset.CreatedAt = time.Now().UTC()
	certAsset.UpdatedAt = certAsset.CreatedAt
	if _, err := s.getStoredAsset(ctx, assetID); err == nil {
		return s.updateAsset(ctx, certAsset)
	} else if !isNotFoundError(err) {
		return err
	}
	return s.insertAsset(ctx, certAsset)
}

func (s *Service) agentTLSAssetsFromStored(ca, client storedAsset) (*agentsecurity.TLSAssets, error) {
	caKey, err := s.decryptPrivateKeyPEM(ca)
	if err != nil {
		return nil, err
	}
	clientKey, err := s.decryptPrivateKeyPEM(client)
	if err != nil {
		return nil, err
	}
	return agentsecurity.NewTLSAssetsFromPEM([]byte(ca.certificateText), caKey, []byte(client.certificateText), clientKey)
}

func (s *Service) importLegacyAgentTLSFiles(ctx context.Context) error {
	dir := filepath.Join(s.cfg.DataRoot, "agent", "tls")
	caPEM, caErr := os.ReadFile(filepath.Join(dir, "ca.pem"))
	caKeyPEM, caKeyErr := os.ReadFile(filepath.Join(dir, "ca-key.pem"))
	if os.IsNotExist(caErr) || os.IsNotExist(caKeyErr) {
		return nil
	}
	if caErr != nil {
		return caErr
	}
	if caKeyErr != nil {
		return caKeyErr
	}
	if err := s.importSystemCertificate(ctx, SystemAgentCAAssetID, "Panel Agent CA", "", "ca", caPEM, caKeyPEM); err != nil {
		return err
	}
	clientPEM, certErr := os.ReadFile(filepath.Join(dir, "panel-client.pem"))
	clientKeyPEM, keyErr := os.ReadFile(filepath.Join(dir, "panel-client-key.pem"))
	if os.IsNotExist(certErr) || os.IsNotExist(keyErr) {
		ca, err := s.getStoredAsset(ctx, SystemAgentCAAssetID)
		if err != nil {
			return err
		}
		_, err = s.createAgentClientCertificate(ctx, ca)
		return err
	}
	if certErr != nil {
		return certErr
	}
	if keyErr != nil {
		return keyErr
	}
	return s.importSystemCertificate(ctx, SystemAgentClientAssetID, "Panel Agent client", SystemAgentCAAssetID, "client", clientPEM, clientKeyPEM)
}

func (s *Service) importSystemCertificate(ctx context.Context, assetID, name, parentID, role string, certPEM, keyPEM []byte) error {
	if _, err := s.getStoredAsset(ctx, assetID); err == nil {
		return nil
	} else if !isNotFoundError(err) {
		return err
	}
	asset, err := s.prepareImportedAsset(ctx, ImportRequest{
		Type:           TypeTLSCertificate,
		Name:           name,
		ParentAssetID:  parentID,
		CertificatePEM: string(certPEM),
		PrivateKeyPEM:  string(keyPEM),
	}, assetID)
	if err != nil {
		return err
	}
	if cert, _, parseErr := parseCertificatePEM(string(certPEM)); parseErr == nil && cert.IsCA {
		asset.Type = TypeCACertificate
		asset.ParentAssetID = ""
	}
	asset.Metadata = systemMetadata(role)
	asset.CreatedAt = time.Now().UTC()
	asset.UpdatedAt = asset.CreatedAt
	return s.insertAsset(ctx, asset)
}

func (s *Service) deleteAgentSystemAssets(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM key_assets WHERE json_extract(metadata_json,'$.systemScope')=?`, systemAgentScope)
	return err
}

func (s *Service) deleteStoredAssetIfExists(ctx context.Context, assetID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM key_assets WHERE id=?`, assetID)
	return err
}

func systemMetadata(role string) map[string]any {
	return map[string]any{
		systemManagedKey: true,
		systemScopeKey:   systemAgentScope,
		systemRoleKey:    role,
		"origin":         "system",
	}
}

func agentServerAssetID(serverID string) string {
	return "agent-server-" + strings.TrimSpace(serverID)
}

func firstNonNil(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func isNotFoundError(err error) bool {
	var pe *panelerr.Error
	return errors.As(err, &pe) && pe.Code == "not_found"
}

func (s *Service) newAgentCAAsset(assetID string) (storedAsset, error) {
	key, err := generateKeyMaterial(AlgorithmEd25519, 0)
	if err != nil {
		return storedAsset{}, err
	}
	privateKeyPEM, err := marshalPrivateKeyPEM(key.private)
	if err != nil {
		return storedAsset{}, err
	}
	now := time.Now().UTC()
	template := newCertificateTemplate("Panel Agent CA", now.Add(-time.Hour), now.Add(agentsecurity.DefaultCAValidity), true, nil, nil)
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
	return storedAsset{
		Asset: Asset{
			ID:          assetID,
			Type:        TypeCACertificate,
			Name:        "Panel Agent CA",
			Algorithm:   key.algorithm,
			KeySize:     key.keySize,
			CommonName:  "Panel Agent CA",
			Fingerprint: certificateFingerprint(cert),
			PublicKey:   publicKeyDisplayForCertificate(material.publicKeyPEM),
			Metadata:    systemMetadata("ca"),
			NotBefore:   cert.NotBefore,
			NotAfter:    cert.NotAfter,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		certificateText:      string(material.certificatePEM),
		privateKeyCiphertext: string(material.privateKeyPEM),
	}, nil
}

func (s *Service) newAgentLeafAsset(assetID, name string, ca storedAsset, role, commonName string, dnsNames []string, ips []net.IP) (storedAsset, error) {
	key, err := generateKeyMaterial(AlgorithmEd25519, 0)
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
	parentPrivateKey, err := s.decryptPrivateKey(ca)
	if err != nil {
		return storedAsset{}, err
	}
	now := time.Now().UTC()
	template := newCertificateTemplate(commonName, now.Add(-time.Hour), now.Add(agentsecurity.DefaultLeafValidity), false, dnsNames, ips)
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
	return storedAsset{
		Asset: Asset{
			ID:            assetID,
			Type:          TypeTLSCertificate,
			Name:          name,
			ParentAssetID: ca.ID,
			Algorithm:     key.algorithm,
			KeySize:       key.keySize,
			CommonName:    commonName,
			DNSNames:      append([]string(nil), dnsNames...),
			Fingerprint:   certificateFingerprint(cert),
			PublicKey:     publicKeyDisplayForCertificate(material.publicKeyPEM),
			Metadata:      systemMetadata(role),
			NotBefore:     cert.NotBefore,
			NotAfter:      cert.NotAfter,
			CreatedAt:     now,
			UpdatedAt:     now,
		},
		certificateText:      string(material.certificatePEM),
		privateKeyCiphertext: string(material.privateKeyPEM),
	}, nil
}
