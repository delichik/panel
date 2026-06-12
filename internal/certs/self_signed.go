package certs

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"panel/internal/id"
	"panel/internal/panelerr"
	"panel/internal/tasks"
)

func (s *Service) CreateSelfSignedCA(ctx context.Context, in SelfSignedCARequest) (SelfSignedCertificate, error) {
	name := strings.TrimSpace(in.Name)
	commonName := strings.TrimSpace(in.CommonName)
	if name == "" || commonName == "" {
		return SelfSignedCertificate{}, panelerr.Validation("self_signed_ca_invalid", "CA name and common name are required")
	}
	years := in.Years
	if years <= 0 {
		years = 10
	}
	if years > 30 {
		return SelfSignedCertificate{}, panelerr.Validation("self_signed_ca_validity_invalid", "CA validity cannot exceed 30 years")
	}
	now := time.Now().UTC()
	cert := SelfSignedCertificate{
		ID: id.New("ca"), Kind: "ca", Name: name, CommonName: commonName,
		NotBefore: now.Add(-time.Hour), NotAfter: now.AddDate(years, 0, 0),
		CreatedAt: now, UpdatedAt: now,
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return SelfSignedCertificate{}, err
	}
	tpl := &x509.Certificate{
		SerialNumber: selfSignedSerial(), Subject: pkix.Name{CommonName: commonName, Organization: []string{"Panel"}},
		NotBefore: cert.NotBefore, NotAfter: cert.NotAfter,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true, IsCA: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return SelfSignedCertificate{}, err
	}
	if err := s.writeSelfSignedFiles(cert.ID, der, key); err != nil {
		return SelfSignedCertificate{}, err
	}
	cert.Fingerprint = certificateFingerprint(der)
	if err := s.insertSelfSigned(ctx, cert); err != nil {
		return SelfSignedCertificate{}, err
	}
	return cert, nil
}

func (s *Service) CreateSelfSignedLeaf(ctx context.Context, in SelfSignedLeafRequest) (SelfSignedCertificate, error) {
	ca, err := s.GetSelfSigned(ctx, strings.TrimSpace(in.CAID))
	if err != nil {
		return SelfSignedCertificate{}, err
	}
	if ca.Kind != "ca" {
		return SelfSignedCertificate{}, panelerr.Validation("self_signed_parent_invalid", "Selected parent is not a certificate authority")
	}
	name := strings.TrimSpace(in.Name)
	commonName := strings.TrimSpace(in.CommonName)
	dnsNames := normalizeNames(in.DNSNames)
	ipAddresses, ips, err := normalizeIPs(in.IPAddresses)
	if err != nil {
		return SelfSignedCertificate{}, err
	}
	if name == "" || commonName == "" || (len(dnsNames) == 0 && len(ipAddresses) == 0) {
		return SelfSignedCertificate{}, panelerr.Validation("self_signed_certificate_invalid", "Name, common name, and at least one DNS name or IP address are required")
	}
	days := in.Days
	if days <= 0 {
		days = 365
	}
	if days > 3650 {
		return SelfSignedCertificate{}, panelerr.Validation("self_signed_certificate_validity_invalid", "Certificate validity cannot exceed 3650 days")
	}
	now := time.Now().UTC()
	cert := SelfSignedCertificate{
		ID: id.New("cert"), ParentCAID: ca.ID, Kind: "leaf", Name: name, CommonName: commonName,
		DNSNames: dnsNames, IPAddresses: ipAddresses,
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Duration(days) * 24 * time.Hour),
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.issueSelfSignedLeaf(&cert, ips); err != nil {
		return SelfSignedCertificate{}, err
	}
	if err := s.insertSelfSigned(ctx, cert); err != nil {
		return SelfSignedCertificate{}, err
	}
	return cert, nil
}

func (s *Service) RenewSelfSignedLeaf(ctx context.Context, certID string) (SelfSignedCertificate, error) {
	cert, err := s.GetSelfSigned(ctx, certID)
	if err != nil {
		return SelfSignedCertificate{}, err
	}
	if cert.Kind != "leaf" {
		return SelfSignedCertificate{}, panelerr.Validation("self_signed_renew_leaf_required", "Only leaf certificates can be reissued")
	}
	taskID := ""
	if s.tasks != nil {
		task, err := s.tasks.Create(ctx, tasks.CreateInput{
			Type:         TaskTypeSelfSignedRenew,
			ResourceType: "certificate",
			ResourceID:   cert.ID,
			TriggerType:  "user",
			Status:       tasks.StatusRunning,
			Summary:      "Reissuing self-signed certificate " + cert.Name,
		})
		if err != nil {
			return SelfSignedCertificate{}, err
		}
		taskID = task.ID
		defer s.tasks.FinishExecution(taskID)
		_ = s.tasks.Advance(ctx, taskID, "issuing", "Issuing certificate from reusable CA")
	}
	fail := func(err error) (SelfSignedCertificate, error) {
		if taskID != "" {
			_ = s.tasks.Fail(ctx, taskID, err)
		}
		return SelfSignedCertificate{}, err
	}
	_, ips, err := normalizeIPs(cert.IPAddresses)
	if err != nil {
		return fail(err)
	}
	duration := cert.NotAfter.Sub(cert.NotBefore)
	now := time.Now().UTC()
	cert.NotBefore = now.Add(-time.Hour)
	cert.NotAfter = now.Add(duration)
	cert.UpdatedAt = now
	if err := s.issueSelfSignedLeaf(&cert, ips); err != nil {
		return fail(err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE self_signed_certificates SET fingerprint=?,not_before=?,not_after=?,updated_at=? WHERE id=?`,
		cert.Fingerprint, formatTime(cert.NotBefore), formatTime(cert.NotAfter), formatTime(cert.UpdatedAt), cert.ID); err != nil {
		return fail(err)
	}
	if taskID != "" {
		_ = s.tasks.Advance(ctx, taskID, "syncing", "Synchronizing applications and reverse proxy")
	}
	if err := s.refreshApplications(ctx); err != nil {
		return fail(err)
	}
	if taskID != "" {
		if err := s.tasks.Complete(ctx, taskID, "Reissued self-signed certificate "+cert.Name); err != nil {
			return SelfSignedCertificate{}, err
		}
	}
	return cert, nil
}

func (s *Service) issueSelfSignedLeaf(cert *SelfSignedCertificate, ips []net.IP) error {
	caCertPath, caKeyPath, _ := s.selfSignedPaths(cert.ParentCAID)
	caPEM, err := os.ReadFile(caCertPath)
	if err != nil {
		return err
	}
	caKeyPEM, err := os.ReadFile(caKeyPath)
	if err != nil {
		return err
	}
	caBlock, _ := pem.Decode(caPEM)
	keyBlock, _ := pem.Decode(caKeyPEM)
	if caBlock == nil || keyBlock == nil {
		return panelerr.Validation("self_signed_ca_files_invalid", "CA certificate or private key is invalid")
	}
	caCert, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		return err
	}
	caKey, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		return err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	tpl := &x509.Certificate{
		SerialNumber: selfSignedSerial(), Subject: pkix.Name{CommonName: cert.CommonName, Organization: []string{"Panel"}},
		NotBefore: cert.NotBefore, NotAfter: cert.NotAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true, DNSNames: cert.DNSNames, IPAddresses: ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, caCert, &key.PublicKey, caKey)
	if err != nil {
		return err
	}
	if err := s.writeSelfSignedFiles(cert.ID, der, key); err != nil {
		return err
	}
	cert.Fingerprint = certificateFingerprint(der)
	return nil
}

func (s *Service) writeSelfSignedFiles(certID string, certDER []byte, key *ecdsa.PrivateKey) error {
	certPath, keyPath, publicPath := s.selfSignedPaths(certID)
	if err := os.MkdirAll(filepath.Dir(certPath), 0o700); err != nil {
		return err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	publicDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return err
	}
	files := []struct {
		path string
		data []byte
		mode os.FileMode
	}{
		{certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}), 0o644},
		{keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600},
		{publicPath, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER}), 0o644},
	}
	for _, file := range files {
		if err := os.WriteFile(file.path, file.data, file.mode); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ListSelfSigned(ctx context.Context) ([]SelfSignedCertificate, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,parent_ca_id,kind,name,common_name,dns_names_json,ip_addresses_json,fingerprint,not_before,not_after,created_at,updated_at FROM self_signed_certificates ORDER BY kind,name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SelfSignedCertificate{}
	for rows.Next() {
		cert, err := scanSelfSigned(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, cert)
	}
	return out, rows.Err()
}

func (s *Service) GetSelfSigned(ctx context.Context, certID string) (SelfSignedCertificate, error) {
	cert, err := scanSelfSigned(s.db.QueryRowContext(ctx, `SELECT id,parent_ca_id,kind,name,common_name,dns_names_json,ip_addresses_json,fingerprint,not_before,not_after,created_at,updated_at FROM self_signed_certificates WHERE id=?`, certID))
	if err == sql.ErrNoRows {
		return SelfSignedCertificate{}, panelerr.NotFound("self-signed certificate")
	}
	return cert, err
}

func (s *Service) DeleteSelfSigned(ctx context.Context, certID string) error {
	cert, err := s.GetSelfSigned(ctx, certID)
	if err != nil {
		return err
	}
	if cert.Kind == "ca" {
		var count int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM self_signed_certificates WHERE parent_ca_id=?`, cert.ID).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			return panelerr.Conflict("self_signed_ca_has_certificates", "Certificate authority still has issued certificates")
		}
	}
	domains := append(append([]string(nil), cert.DNSNames...), cert.IPAddresses...)
	if used, err := s.certificateInUse(ctx, cert.ID, domains, ""); err != nil {
		return err
	} else if used {
		return panelerr.Conflict("certificate_in_use", "Certificate is still used by an application or reverse proxy")
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM self_signed_certificates WHERE id=?`, cert.ID); err != nil {
		return err
	}
	certPath, _, _ := s.selfSignedPaths(cert.ID)
	_ = os.RemoveAll(filepath.Dir(certPath))
	return nil
}

func (s *Service) insertSelfSigned(ctx context.Context, cert SelfSignedCertificate) error {
	dnsNames, _ := json.Marshal(cert.DNSNames)
	ipAddresses, _ := json.Marshal(cert.IPAddresses)
	certPath, keyPath, publicPath := s.selfSignedPaths(cert.ID)
	_, err := s.db.ExecContext(ctx, `INSERT INTO self_signed_certificates(id,parent_ca_id,kind,name,common_name,dns_names_json,ip_addresses_json,certificate_path,private_key_path,public_key_path,fingerprint,not_before,not_after,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		cert.ID, cert.ParentCAID, cert.Kind, cert.Name, cert.CommonName, string(dnsNames), string(ipAddresses), certPath, keyPath, publicPath, cert.Fingerprint, formatTime(cert.NotBefore), formatTime(cert.NotAfter), formatTime(cert.CreatedAt), formatTime(cert.UpdatedAt))
	return err
}

func (s *Service) selfSignedPaths(certID string) (string, string, string) {
	dir := filepath.Join(s.dataRoot, "certs", "self-signed", certID)
	return filepath.Join(dir, "certificate.pem"), filepath.Join(dir, "private-key.pem"), filepath.Join(dir, "public-key.pem")
}

type selfSignedScanner interface{ Scan(...any) error }

func scanSelfSigned(row selfSignedScanner) (SelfSignedCertificate, error) {
	var cert SelfSignedCertificate
	var dnsNames, ipAddresses, notBefore, notAfter, createdAt, updatedAt string
	err := row.Scan(&cert.ID, &cert.ParentCAID, &cert.Kind, &cert.Name, &cert.CommonName, &dnsNames, &ipAddresses, &cert.Fingerprint, &notBefore, &notAfter, &createdAt, &updatedAt)
	if err != nil {
		return SelfSignedCertificate{}, err
	}
	_ = json.Unmarshal([]byte(dnsNames), &cert.DNSNames)
	_ = json.Unmarshal([]byte(ipAddresses), &cert.IPAddresses)
	cert.NotBefore = parseTime(notBefore)
	cert.NotAfter = parseTime(notAfter)
	cert.CreatedAt = parseTime(createdAt)
	cert.UpdatedAt = parseTime(updatedAt)
	return cert, nil
}

func normalizeNames(values []string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, exists := seen[value]; !exists {
			seen[value] = struct{}{}
			out = append(out, value)
		}
	}
	return out
}

func normalizeIPs(values []string) ([]string, []net.IP, error) {
	seen := map[string]struct{}{}
	text := []string{}
	ips := []net.IP{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		ip := net.ParseIP(value)
		if ip == nil {
			return nil, nil, panelerr.Validation("self_signed_ip_invalid", "Self-signed certificate IP address is invalid")
		}
		normalized := ip.String()
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		text = append(text, normalized)
		ips = append(ips, ip)
	}
	return text, ips, nil
}

func selfSignedSerial() *big.Int {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return big.NewInt(time.Now().UnixNano())
	}
	return serial
}

func certificateFingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}
