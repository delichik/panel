package keyassets

import "time"

const (
	TypeCACertificate  = "ca_certificate"
	TypeTLSCertificate = "tls_certificate"
	TypeSSHKeyPair     = "ssh_key_pair"

	AlgorithmEd25519 = "ed25519"
	AlgorithmRSA     = "rsa"

	TaskTypeTLSReissue        = "key_asset_tls_reissue"
	TaskTypePanelTLSReconcile = "panel_tls_reconcile"
	TaskTypeSSHRegenerate     = "key_asset_ssh_regenerate"
	TaskTypeExport            = "key_asset_export"
	TaskTypeImport            = "key_asset_import"
)

type Asset struct {
	ID            string           `json:"id"`
	Type          string           `json:"type"`
	Name          string           `json:"name"`
	ParentAssetID string           `json:"parentAssetId,omitempty"`
	Algorithm     string           `json:"algorithm"`
	KeySize       int              `json:"keySize,omitempty"`
	CommonName    string           `json:"commonName,omitempty"`
	DNSNames      []string         `json:"dnsNames,omitempty"`
	IPAddresses   []string         `json:"ipAddresses,omitempty"`
	Fingerprint   string           `json:"fingerprint"`
	PublicKey     string           `json:"publicKey,omitempty"`
	Metadata      map[string]any   `json:"metadata,omitempty"`
	NotBefore     time.Time        `json:"notBefore,omitempty"`
	NotAfter      time.Time        `json:"notAfter,omitempty"`
	InUse         bool             `json:"inUse"`
	HasChildren   bool             `json:"hasChildren"`
	ChildCount    int              `json:"childCount"`
	References    []AssetReference `json:"references"`
	CanReissue    bool             `json:"canReissue"`
	CanRegenerate bool             `json:"canRegenerate"`
	FileKinds     []string         `json:"fileKinds"`
	CreatedAt     time.Time        `json:"createdAt"`
	UpdatedAt     time.Time        `json:"updatedAt"`
}

type AssetReference struct {
	ResourceType string `json:"resourceType"`
	ResourceID   string `json:"resourceId"`
	ResourceName string `json:"resourceName"`
	Relation     string `json:"relation"`
}

type CreateCARequest struct {
	Name         string `json:"name"`
	CommonName   string `json:"commonName"`
	Algorithm    string `json:"algorithm"`
	KeySize      int    `json:"keySize"`
	Years        int    `json:"years"`
	ValidityDays int    `json:"validityDays"`
}

type CreateTLSRequest struct {
	Name          string   `json:"name"`
	ParentAssetID string   `json:"parentAssetId"`
	CAID          string   `json:"caId"`
	CommonName    string   `json:"commonName"`
	Algorithm     string   `json:"algorithm"`
	KeySize       int      `json:"keySize"`
	DNSNames      []string `json:"dnsNames"`
	IPAddresses   []string `json:"ipAddresses"`
	Days          int      `json:"days"`
	ValidityDays  int      `json:"validityDays"`
}

type GenerateSSHRequest struct {
	Name      string `json:"name"`
	Algorithm string `json:"algorithm"`
	KeySize   int    `json:"keySize"`
	Comment   string `json:"comment"`
}

type ImportRequest struct {
	Type           string `json:"type"`
	Name           string `json:"name"`
	ParentAssetID  string `json:"parentAssetId"`
	CommonName     string `json:"commonName"`
	Algorithm      string `json:"algorithm"`
	KeySize        int    `json:"keySize"`
	CertificatePEM string `json:"certificatePem"`
	PrivateKeyPEM  string `json:"privateKeyPem"`
	PublicKeyPEM   string `json:"publicKeyPem"`
	PublicKey      string `json:"publicKey"`
}

type ReissueResult struct {
	Asset  Asset  `json:"asset"`
	TaskID string `json:"taskId,omitempty"`
}

type RegenerateResult struct {
	Asset  Asset  `json:"asset"`
	TaskID string `json:"taskId,omitempty"`
}

type ExportRequest struct {
	AssetIDs []string `json:"assetIds"`
	Password string   `json:"password"`
}

type ExportResult struct {
	TaskID string `json:"taskId"`
}

type ImportPreflightRequest struct {
	ArchiveBase64 string `json:"archiveBase64"`
	Password      string `json:"password"`
}

type ImportPreflightResult struct {
	PlanID         string             `json:"planId"`
	ExpiresAt      time.Time          `json:"expiresAt"`
	Assets         []ImportAssetCheck `json:"assets"`
	Conflicts      []ImportConflict   `json:"conflicts"`
	OverwriteInUse []string           `json:"overwriteInUse"`
}

type ImportAssetCheck struct {
	ID            string `json:"id"`
	Type          string `json:"type"`
	Name          string `json:"name"`
	ParentAssetID string `json:"parentAssetId,omitempty"`
	Algorithm     string `json:"algorithm,omitempty"`
	KeySize       int    `json:"keySize,omitempty"`
	CommonName    string `json:"commonName,omitempty"`
	Fingerprint   string `json:"fingerprint,omitempty"`
	StandaloneTLS bool   `json:"standaloneTls"`
}

type ImportConflict struct {
	IncomingID     string `json:"incomingId"`
	IncomingName   string `json:"incomingName"`
	ExistingID     string `json:"existingId,omitempty"`
	ExistingName   string `json:"existingName,omitempty"`
	ConflictByID   bool   `json:"conflictById"`
	ConflictByName bool   `json:"conflictByName"`
}

type ImportExecuteRequest struct {
	Strategy                  string                     `json:"strategy"`
	ConfirmOverwriteInUse     bool                       `json:"confirmOverwriteInUse"`
	ConfirmDangerousOverwrite bool                       `json:"confirmDangerousOverwrite"`
	Resolutions               []ImportConflictResolution `json:"resolutions"`
}

type ImportExecuteResult struct {
	TaskID      string `json:"taskId"`
	OperationID string `json:"operationId,omitempty"`
}

type ImportConflictResolution struct {
	AssetID       string `json:"assetId"`
	Action        string `json:"action"`
	TargetAssetID string `json:"targetAssetId,omitempty"`
}
