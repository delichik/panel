package dns

import "context"

type Record struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Value   string `json:"value"`
	TTL     int    `json:"ttl,omitempty"`
	Proxied bool   `json:"proxied,omitempty"`
	Comment string `json:"comment,omitempty"`
}

type RecordInput struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Value   string `json:"value"`
	TTL     int    `json:"ttl,omitempty"`
	Proxied *bool  `json:"proxied,omitempty"`
	Comment string `json:"comment,omitempty"`
}

type Provider interface {
	ListRecords(ctx context.Context, zone string) ([]Record, error)
	CreateRecord(ctx context.Context, zone string, record RecordInput) (Record, error)
	UpdateRecord(ctx context.Context, zone string, id string, record RecordInput) (Record, error)
	DeleteRecord(ctx context.Context, zone string, id string) error
}
