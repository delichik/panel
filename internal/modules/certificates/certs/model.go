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

	TaskTypeIssue           = "certificate_issue"
	TaskTypeRenew           = "certificate_renew"
	TaskTypeSelfSignedRenew = "certificate_self_signed_renew"
)

type Certificate struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	DomainID        string    `json:"domainId"`
	Domain          string    `json:"domain"`
	Prefix          string    `json:"prefix"`
	Scope           string    `json:"scope"`
	Domains         []string  `json:"domains"`
	VariableName    string    `json:"variableName"`
	CertificatePath string    `json:"certificatePath"`
	PrivateKeyPath  string    `json:"privateKeyPath"`
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
	Name         string   `json:"name"`
	DomainID     string   `json:"domainId"`
	Prefix       string   `json:"prefix"`
	Prefixes     []string `json:"prefixes"`
	Scope        string   `json:"scope"`
	VariableName string   `json:"variableName"`
}

type IssueResult struct {
	Certificate Certificate `json:"certificate"`
	TaskID      string      `json:"taskId,omitempty"`
}

type BuiltinVariable struct {
	CertificatePEM string   `json:"certificatePem"`
	PrivateKeyPEM  string   `json:"privateKeyPem"`
	CAChainPEM     string   `json:"caChainPem,omitempty"`
	Domains        []string `json:"domains"`
}
