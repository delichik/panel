package dns

import "context"

type Record struct {
	ID    string
	Name  string
	Type  string
	Value string
}

type RecordInput struct {
	Name  string
	Type  string
	Value string
}

type Provider interface {
	ListRecords(ctx context.Context, zone string) ([]Record, error)
	CreateRecord(ctx context.Context, zone string, record RecordInput) (Record, error)
	UpdateRecord(ctx context.Context, zone string, id string, record RecordInput) (Record, error)
	DeleteRecord(ctx context.Context, zone string, id string) error
}
