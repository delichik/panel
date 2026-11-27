package logging

import (
	"context"
	"fmt"
	"strings"

	"go.uber.org/zap"
)

// Process states used by ENG-EVT-010.
const (
	ProcessQueued    = "queued"
	ProcessRunning   = "running"
	ProcessSucceeded = "succeeded"
	ProcessFailed    = "failed"
	ProcessCancelled = "cancelled"
)

// Resource is a named resource snapshot for events.resources_json (ENG-EVT-010).
// JSON tags match activitylog.Resource so existing readers stay compatible.
type Resource struct {
	Type       string         `json:"resourceType"`
	ID         string         `json:"resourceId"`
	Role       string         `json:"role,omitempty"`
	Name       string         `json:"nameSnapshot,omitempty"`
	RevisionID string         `json:"revisionId,omitempty"`
	Generation int            `json:"generation,omitempty"`
	Snapshot   map[string]any `json:"snapshot,omitempty"`
}

// ProcessSpec describes a readable process event upsert (ENG-EVT-010).
type ProcessSpec struct {
	EventID     string
	Module      string
	Origin      string
	OperationID string
	ExecutionID string
	RequestID   string
	Title       string
	Summary     string
	State       string
	Level       string
	HadError    bool
	Finished    bool
	EventType   string // machine code for JSONL only; never used as Title alone
	Actor       map[string]string
	Resources   any // []Resource or compatible activitylog.Resource slices
	Fields      []zap.Field // extra diagnostic fields for the JSONL line
	SkipJSONL   bool        // upsert process row only (task log readers share eventId)
}

// EmitProcess registers/updates a process row and writes one JSONL line under the
// same eventId so /events → /logs?eventId= stays linked.
func EmitProcess(ctx context.Context, spec ProcessSpec) context.Context {
	ctx, meta := prepareProcess(ctx, spec)
	_ = RecordContext(ctx, meta)
	if !spec.SkipJSONL {
		logProcessLine(ctx, spec)
	}
	return ctx
}

// TouchProcess updates an existing process row (same eventId) and writes a JSONL line.
// Callers should keep Title stable for a lifecycle; Summary/State should advance.
func TouchProcess(ctx context.Context, spec ProcessSpec) context.Context {
	return EmitProcess(ctx, spec)
}

func prepareProcess(ctx context.Context, spec ProcessSpec) (context.Context, ContextMeta) {
	eventID := strings.TrimSpace(spec.EventID)
	module := strings.TrimSpace(spec.Module)
	if module == "" {
		module = "process"
	}
	origin := strings.TrimSpace(spec.Origin)
	if origin == "" {
		origin = "process"
	}
	state := strings.TrimSpace(spec.State)
	if state == "" {
		if spec.Finished {
			if spec.HadError {
				state = ProcessFailed
			} else {
				state = ProcessSucceeded
			}
		} else {
			state = ProcessRunning
		}
	}
	title := strings.TrimSpace(spec.Title)
	summary := strings.TrimSpace(spec.Summary)
	if title == "" {
		title = "Process update"
	}
	if summary == "" || summary == title {
		if code := strings.TrimSpace(spec.EventType); code != "" && code != title {
			summary = code
		} else {
			summary = state
		}
	}
	level := strings.TrimSpace(spec.Level)
	if level == "" {
		if spec.HadError {
			level = "warn"
		} else {
			level = "info"
		}
	}
	ctx = Ensure(ctx, Context{
		EventID:     eventID,
		StreamID:    eventID,
		Module:      module,
		OperationID: strings.TrimSpace(spec.OperationID),
		ExecutionID: strings.TrimSpace(spec.ExecutionID),
		RequestID:   strings.TrimSpace(spec.RequestID),
	})
	meta := ContextMeta{
		Origin:        origin,
		Title:         title,
		Summary:       summary,
		State:         state,
		Level:         level,
		HadError:      spec.HadError,
		Finished:      spec.Finished,
		ActorJSON:     MarshalActorJSON(spec.Actor),
		ResourcesJSON: MarshalResourcesJSON(spec.Resources),
	}
	return ctx, meta
}

func logProcessLine(ctx context.Context, spec ProcessSpec) {
	msg := strings.TrimSpace(spec.Summary)
	if msg == "" {
		msg = strings.TrimSpace(spec.Title)
	}
	fields := make([]zap.Field, 0, 8+len(spec.Fields))
	if code := strings.TrimSpace(spec.EventType); code != "" {
		fields = append(fields, zap.String("eventType", code))
	}
	if id := strings.TrimSpace(spec.OperationID); id != "" {
		fields = append(fields, zap.String("operationId", id))
	}
	if id := strings.TrimSpace(spec.ExecutionID); id != "" {
		fields = append(fields, zap.String("executionId", id))
	}
	if state := strings.TrimSpace(spec.State); state != "" {
		fields = append(fields, zap.String("processState", state))
	}
	fields = append(fields, spec.Fields...)
	logger := From(ctx)
	switch {
	case spec.HadError || strings.EqualFold(spec.Level, "error"):
		logger.Error(msg, fields...)
	case strings.EqualFold(spec.Level, "warn"), strings.EqualFold(spec.Level, "warning"):
		logger.Warn(msg, fields...)
	default:
		logger.Info(msg, fields...)
	}
}

// ResourceRef is a convenience builder for Resource with a name.
func ResourceRef(typ, id, name, role string) Resource {
	return Resource{
		Type: typ,
		ID:   strings.TrimSpace(id),
		Name: strings.TrimSpace(name),
		Role: strings.TrimSpace(role),
	}
}

// DeployTitle builds ENG-EVT-010 titles like "Deploy hermes → Oracle Phoenix".
func DeployTitle(action, appName, serverName string) string {
	verb := strings.TrimSpace(action)
	switch verb {
	case "", "apply":
		verb = "Deploy"
	case "stop":
		verb = "Stop"
	case "purge":
		verb = "Purge"
	default:
		verb = strings.ToUpper(verb[:1]) + verb[1:]
	}
	appName = firstNonEmpty(strings.TrimSpace(appName), "application")
	serverName = firstNonEmpty(strings.TrimSpace(serverName), "server")
	return fmt.Sprintf("%s %s → %s", verb, appName, serverName)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
