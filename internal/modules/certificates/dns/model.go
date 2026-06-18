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

type SaveDomainRequest struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
	APIToken string `json:"apiToken"`
}
