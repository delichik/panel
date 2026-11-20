package dns

import "time"

const ProviderCloudflare = "cloudflare"

type Domain struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Provider  string    `json:"provider"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ResolvedDomain struct {
	Domain
	APIToken string
}

type RecordSnapshot struct {
	Items            []Record   `json:"items"`
	ObservedAt       *time.Time `json:"observedAt,omitempty"`
	Stale            bool       `json:"stale"`
	Refreshing       bool       `json:"refreshing"`
	RefreshTaskID    string     `json:"refreshTaskId,omitempty"`
	LastRefreshError string     `json:"lastRefreshError,omitempty"`
}

type SaveDomainRequest struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
	APIToken string `json:"apiToken"`
}
