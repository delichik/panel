package logging

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// ContextMeta is optional stream metadata upserted into log_contexts.
type ContextMeta struct {
	Origin        string
	Title         string
	Summary       string
	Result        string
	HadError      bool
	ActorJSON     string
	ResourcesJSON string
	LastSourceSeq int64
	Finished      bool
}

// RecordContext upserts log_contexts for the stream on ctx using meta fields.
// Missing stream/module or store is a no-op.
func RecordContext(ctx context.Context, meta ContextMeta) error {
	logCtx, ok := Lookup(ctx)
	if !ok {
		return nil
	}
	store := getContextStore()
	if store == nil {
		return nil
	}
	origin := strings.TrimSpace(meta.Origin)
	if origin == "" {
		origin = "process"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	rec := ContextRecord{
		StreamID:      logCtx.StreamID,
		Module:        logCtx.Module,
		Origin:        origin,
		OperationID:   logCtx.OperationID,
		ExecutionID:   logCtx.ExecutionID,
		RequestID:     logCtx.RequestID,
		Title:         meta.Title,
		Summary:       meta.Summary,
		Result:        meta.Result,
		HadError:      meta.HadError,
		ActorJSON:     meta.ActorJSON,
		ResourcesJSON: meta.ResourcesJSON,
		LastSourceSeq: meta.LastSourceSeq,
		LogFile:       CurrentLogFileName(),
		UpdatedAt:     now,
	}
	if meta.Finished {
		rec.FinishedAt = now
	}
	return store.Upsert(ctx, rec)
}

// MarshalActorJSON encodes an actor map for log_contexts.actor_json.
func MarshalActorJSON(actor map[string]string) string {
	if len(actor) == 0 {
		return ""
	}
	b, err := json.Marshal(actor)
	if err != nil {
		return ""
	}
	return string(b)
}

// MarshalResourcesJSON encodes resources for log_contexts.resources_json.
func MarshalResourcesJSON(resources any) string {
	if resources == nil {
		return ""
	}
	b, err := json.Marshal(resources)
	if err != nil {
		return ""
	}
	return string(b)
}
