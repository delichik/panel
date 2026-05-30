package certs

import "time"

const (
	ScopeSingle   = "single"
	ScopeWildcard = "wildcard"

	TaskTypeIssue = "certificate_issue"
	TaskTypeRenew = "certificate_renew"
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
	AutoRenew       bool      `json:"autoRenew"`
	NextRenewAt     time.Time `json:"nextRenewAt,omitempty"`
	NotBefore       time.Time `json:"notBefore,omitempty"`
	NotAfter        time.Time `json:"notAfter,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type IssueRequest struct {
	Name         string `json:"name"`
	DomainID     string `json:"domainId"`
	Prefix       string `json:"prefix"`
	Scope        string `json:"scope"`
	VariableName string `json:"variableName"`
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
