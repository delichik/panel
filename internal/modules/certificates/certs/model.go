package certs

import "time"

const (
	ScopeSingle   = "single"
	ScopeWildcard = "wildcard"
	ScopePrefixes = "prefixes"

	StatusPending = "pending"
	StatusIssuing = "issuing"
	StatusIssued  = "issued"
	StatusFailed  = "failed"

	TaskTypeIssue   = "certificate_issue"
	TaskTypeReissue = "certificate_reissue"
	TaskTypeRenew   = "certificate_renew"
)

type Certificate struct {
	ID              string    `json:"id"`
	AssetID         string    `json:"assetId"`
	Name            string    `json:"name"`
	DomainID        string    `json:"domainId"`
	Domain          string    `json:"domain"`
	Prefix          string    `json:"prefix"`
	Scope           string    `json:"scope"`
	Domains         []string  `json:"domains"`
	VariableName    string    `json:"-"`
	CertificatePath string    `json:"-"`
	PrivateKeyPath  string    `json:"-"`
	Issuer          string    `json:"issuer"`
	Status          string    `json:"status"`
	LastError       string    `json:"lastError,omitempty"`
	AutoRenew       bool      `json:"autoRenew"`
	NextRenewAt     time.Time `json:"nextRenewAt,omitempty"`
	NotBefore       time.Time `json:"notBefore,omitempty"`
	NotAfter        time.Time `json:"notAfter,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type CertificateSummary struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	DomainID    string    `json:"domainId"`
	Domain      string    `json:"domain"`
	Prefix      string    `json:"prefix"`
	Scope       string    `json:"scope"`
	Domains     []string  `json:"domains"`
	Issuer      string    `json:"issuer"`
	Status      string    `json:"status"`
	LastError   string    `json:"lastError,omitempty"`
	AutoRenew   bool      `json:"autoRenew"`
	NextRenewAt time.Time `json:"nextRenewAt,omitempty"`
	NotBefore   time.Time `json:"notBefore,omitempty"`
	NotAfter    time.Time `json:"notAfter,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type SelfSignedCertificate struct {
	ID          string    `json:"id"`
	ParentCAID  string    `json:"parentCaId,omitempty"`
	Kind        string    `json:"kind"`
	Name        string    `json:"name"`
	CommonName  string    `json:"commonName"`
	DNSNames    []string  `json:"dnsNames"`
	IPAddresses []string  `json:"ipAddresses"`
	Fingerprint string    `json:"fingerprint"`
	NotBefore   time.Time `json:"notBefore"`
	NotAfter    time.Time `json:"notAfter"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type SelfSignedCARequest struct {
	Name       string `json:"name"`
	CommonName string `json:"commonName"`
	Years      int    `json:"years"`
}

type SelfSignedLeafRequest struct {
	Name        string   `json:"name"`
	CAID        string   `json:"caId"`
	CommonName  string   `json:"commonName"`
	DNSNames    []string `json:"dnsNames"`
	IPAddresses []string `json:"ipAddresses"`
	Days        int      `json:"days"`
}

type IssueRequest struct {
	Name     string   `json:"name"`
	DomainID string   `json:"domainId"`
	Prefix   string   `json:"prefix"`
	Prefixes []string `json:"prefixes"`
	Scope    string   `json:"scope"`
}

type IssueResult struct {
	Certificate Certificate `json:"certificate"`
	TaskID      string      `json:"taskId,omitempty"`
}
