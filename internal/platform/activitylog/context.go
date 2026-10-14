package activitylog

import (
	"context"
	"sync"
)

type actorKey struct{}

func WithActor(ctx context.Context, actor Actor) context.Context {
	return context.WithValue(ctx, actorKey{}, actor)
}
func ActorFromContext(ctx context.Context) Actor { a, _ := ctx.Value(actorKey{}).(Actor); return a }

type receiptKey struct{}
type CapturedReceipt struct {
	mu          sync.Mutex
	OperationID string
	EventID     string
	Seq         int64
}

func WithReceipt(ctx context.Context) context.Context {
	return context.WithValue(ctx, receiptKey{}, &CapturedReceipt{})
}
func RecordReceipt(ctx context.Context, operationID, eventID string, seq int64) {
	value, _ := ctx.Value(receiptKey{}).(*CapturedReceipt)
	if value == nil {
		return
	}
	value.mu.Lock()
	defer value.mu.Unlock()
	if value.OperationID == "" || value.OperationID == operationID {
		value.OperationID = operationID
		value.EventID = eventID
		value.Seq = seq
	}
}
func ReceiptFromContext(ctx context.Context) (operationID, eventID string, seq int64) {
	value, _ := ctx.Value(receiptKey{}).(*CapturedReceipt)
	if value == nil {
		return
	}
	value.mu.Lock()
	defer value.mu.Unlock()
	return value.OperationID, value.EventID, value.Seq
}

type causeKey struct{}

func WithCause(ctx context.Context, eventID string) context.Context {
	return context.WithValue(ctx, causeKey{}, eventID)
}
func CauseFromContext(ctx context.Context) string {
	value, _ := ctx.Value(causeKey{}).(string)
	return value
}
