package logging

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"panel/internal/platform/notifications"
)

// ContextMeta is optional event metadata upserted into events.
type ContextMeta struct {
	Origin        string
	Title         string
	Summary       string
	State         string
	Result        string // deprecated alias for State
	Level         string
	HadError      bool
	ActorJSON     string
	ResourcesJSON string
	LastSourceSeq int64
	Finished      bool
}

// RecordContext upserts events for the event on ctx using meta fields.
// Missing event/module or store is a no-op for events rows.
//
// The process notification channel (ENG-EVT-011) is fed after the row is built
// and before it is upserted, so event notifications do not depend on the events
// store and are delivered even when it is unavailable. Ordinary log lines and
// Touch calls never publish notifications.
func RecordContext(ctx context.Context, meta ContextMeta) error {
	logCtx, ok := Lookup(ctx)
	if !ok {
		return nil
	}
	origin := strings.TrimSpace(meta.Origin)
	if origin == "" {
		origin = "process"
	}
	state := strings.TrimSpace(meta.State)
	if state == "" {
		state = strings.TrimSpace(meta.Result)
	}
	eventID := logCtx.EventID
	if eventID == "" {
		eventID = logCtx.StreamID
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	rec := ContextRecord{
		EventID:       eventID,
		Module:        logCtx.Module,
		Origin:        origin,
		OperationID:   logCtx.OperationID,
		ExecutionID:   logCtx.ExecutionID,
		RequestID:     logCtx.RequestID,
		Title:         meta.Title,
		Summary:       meta.Summary,
		State:         state,
		Level:         meta.Level,
		HadError:      meta.HadError,
		ActorJSON:     meta.ActorJSON,
		ResourcesJSON: meta.ResourcesJSON,
		LastSourceSeq: meta.LastSourceSeq,
		LogFile:       CurrentLogFileName(),
		UpdatedAt:     now,
	}
	if meta.Finished {
		rec.FinishedAt = now
		if rec.State == "" {
			if meta.HadError {
				rec.State = "failed"
			} else {
				rec.State = "succeeded"
			}
		}
	}
	// Publish before Upsert: the notification channel must not wait on (or
	// require) the events database, and a later Upsert failure must not swallow
	// the user-visible lifecycle notification.
	notifications.PublishProcess(notifications.ProcessEvent{
		EventID:   rec.EventID,
		Module:    rec.Module,
		Title:     rec.Title,
		Summary:   rec.Summary,
		State:     rec.State,
		Level:     rec.Level,
		Origin:    rec.Origin,
		HadError:  rec.HadError,
		UpdatedAt: rec.UpdatedAt,
	})
	store := getContextStore()
	if store == nil {
		return nil
	}
	return store.Upsert(ctx, rec)
}

// MarshalActorJSON encodes an actor map for events.actor_json.
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

// MarshalResourcesJSON encodes resources for events.resources_json.
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
