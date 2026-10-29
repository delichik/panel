package panel

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"panel/internal/modules/tasks"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"panel/internal/modules/activity"
	auth "panel/internal/modules/identity"
	"panel/internal/modules/runtimeevents"
	"panel/internal/platform/activitylog"
	"panel/internal/platform/events"
	"panel/internal/platform/logging"
	"panel/internal/platform/maintenance"
)

// systemEventWriter writes system observations via unified logging.
type systemEventWriter struct {
	maintenance maintenance.Gate
	service     *activity.Service
	mu          sync.Mutex
	cancel      context.CancelFunc
	done        chan struct{}
	stop        chan struct{}
	closing     bool
	writers     sync.WaitGroup
}

func (w *systemEventWriter) Log(ctx context.Context, in runtimeevents.WriteEventInput) {
	done, admitted := w.maintenance.Enter()
	if !admitted {
		return
	}
	defer done()
	w.mu.Lock()
	if w.closing {
		w.mu.Unlock()
		return
	}
	if w.stop == nil {
		w.stop = make(chan struct{})
	}
	w.writers.Add(1)
	w.mu.Unlock()
	defer w.writers.Done()

	streamID := logging.NewStreamID()
	ctx = logging.With(ctx, logging.Context{StreamID: streamID, Module: "system"})
	fields := []zap.Field{
		zap.String("eventType", in.EventType),
		zap.String("source", in.Source),
		zap.String("sourceModule", in.SourceModule),
		zap.String("severity", in.Severity),
	}
	if in.ResourceID != "" {
		fields = append(fields, zap.String("resourceId", in.ResourceID), zap.String("resourceName", in.ResourceName))
	}
	msg := in.Summary
	if msg == "" {
		msg = in.EventType
	}
	// Point-in-time system observations (agent.connected/disconnected, etc.)
	// are logs only — they must not register a process row in events.
	logger := logging.From(ctx)
	switch strings.ToLower(in.Severity) {
	case "error":
		logger.Error(msg, fields...)
	case "warning", "warn":
		logger.Warn(msg, fields...)
	default:
		logger.Info(msg, fields...)
	}
}

func (w *systemEventWriter) Start(ctx context.Context) {
	ctx, w.cancel = context.WithCancel(ctx)
	w.done = make(chan struct{})
	go func() {
		defer close(w.done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Projection catch-up retired; keep tick for maintenance gate liveness.
				done, admitted := w.maintenance.Enter()
				if !admitted {
					continue
				}
				done()
			}
		}
	}()
}
func (w *systemEventWriter) Stop() {
	w.mu.Lock()
	if !w.closing {
		w.closing = true
		if w.stop != nil {
			close(w.stop)
		}
	}
	w.mu.Unlock()
	if w.cancel != nil {
		w.cancel()
		<-w.done
	}
	w.writers.Wait()
}
func (a *App) activityAuth(next http.Handler) http.Handler {
	return a.auth.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r.Context())
		streamID := logging.NewStreamID()
		if existing, ok := logging.Lookup(r.Context()); ok && existing.StreamID != "" {
			streamID = existing.StreamID
		}
		ctx := activitylog.WithReceipt(activitylog.WithActor(r.Context(), activitylog.Actor{Kind: "user", ID: "admin", Name: sess.Username}))
		ctx = logging.With(ctx, logging.Context{
			StreamID:  streamID,
			Module:    "http",
			RequestID: streamID,
		})
		mutation := r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch || r.Method == http.MethodDelete
		receivedID := ""
		if mutation && ctx.Value(runtimeClearAuditContextKey{}) != true {
			receivedID = streamID
			logging.From(ctx).Info("http request received",
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
				zap.String("route", r.Pattern),
				zap.String("domain", requestDomain(r)),
				zap.String("eventType", "request.received"),
			)
			_ = logging.RecordContext(ctx, logging.ContextMeta{
				Origin:    events.OriginUser,
				Title:     r.Method + " " + r.URL.Path,
				Summary:   "request received",
				ActorJSON: logging.MarshalActorJSON(map[string]string{"kind": "user", "id": "admin", "name": sess.Username}),
			})
			activitylog.RecordReceipt(ctx, streamID, receivedID, 0)
			ctx = activitylog.WithCause(ctx, receivedID)
			w.Header().Set("X-Operation-Id", streamID)
		}
		capture := &activityResponse{ResponseWriter: w}
		next.ServeHTTP(capture, r.WithContext(ctx))
		if receivedID != "" {
			a.finishRequest(ctx, r, capture, receivedID)
		}
		capture.finish(ctx, a.activity)
	}))
}

func activityCommands(service *tasks.Service, db *sql.DB) func(context.Context, []activity.Execution) []activity.Command {
	return func(ctx context.Context, executions []activity.Execution) []activity.Command {
		out := []activity.Command{}
		seen := map[string]bool{}
		for _, execution := range executions {
			taskID, _, _ := strings.Cut(execution.ExecutionID, ":attempt:")
			if seen[taskID] {
				continue
			}
			seen[taskID] = true
			task, err := service.Get(ctx, taskID)
			if err != nil {
				var count int
				if db != nil && db.QueryRowContext(ctx, `SELECT COUNT(*) FROM event_runs WHERE type='application_deploy' AND execution_id=? AND status='running' AND error_class='uncertainty'`, execution.ExecutionID).Scan(&count) == nil && count > 0 {
					out = append(out, activity.Command{Kind: "resolve", ExecutionID: execution.ExecutionID})
				}
				continue
			}
			if task.Status == tasks.StatusRunning && task.Stage == "uncertain" {
				out = append(out, activity.Command{Kind: "resolve", ExecutionID: task.ID})
				continue
			}
			def, ok := service.Registry().Definition(task.Type)
			if !ok || def.Execute == nil {
				continue
			}
			if def.AllowRetry && (task.Status == tasks.StatusFailed || task.Status == tasks.StatusFailedRetryable || task.Status == tasks.StatusBlocked) {
				out = append(out, activity.Command{Kind: "retry", ExecutionID: task.ID})
			}
			if def.AllowRunNow && (task.Status == tasks.StatusQueued || task.Status == tasks.StatusScheduled || task.Status == tasks.StatusFailedRetryable) {
				out = append(out, activity.Command{Kind: "run-now", ExecutionID: task.ID})
			}
		}
		return out
	}
}

type activityResponse struct {
	http.ResponseWriter
	status    int
	buffer    bytes.Buffer
	streaming bool
}

func (w *activityResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *activityResponse) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	if w.streaming {
		w.ResponseWriter.WriteHeader(status)
	}
}
func (w *activityResponse) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	if w.streaming {
		return w.ResponseWriter.Write(b)
	}
	if w.buffer.Len()+len(b) > 1024*1024 || !strings.Contains(w.Header().Get("Content-Type"), "application/json") {
		w.flushBuffer()
		return w.ResponseWriter.Write(b)
	}
	return w.buffer.Write(b)
}
func (w *activityResponse) flushBuffer() {
	if !w.streaming {
		w.streaming = true
		if w.status == 0 {
			w.status = http.StatusOK
		}
		w.ResponseWriter.WriteHeader(w.status)
		_, _ = w.ResponseWriter.Write(w.buffer.Bytes())
		w.buffer.Reset()
	}
}
func (w *activityResponse) Flush() {
	w.flushBuffer()
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
func (w *activityResponse) finish(ctx context.Context, _ *activity.Service) {
	if w.streaming {
		return
	}
	body := w.buffer.Bytes()
	operationID, eventID, seq := activitylog.ReceiptFromContext(ctx)
	if eventID != "" {
		var envelope map[string]any
		if json.Unmarshal(body, &envelope) == nil {
			key := "data"
			if w.status >= 400 {
				key = "error"
			}
			if value, ok := envelope[key].(map[string]any); ok {
				if operationID != "" {
					value["operationId"] = operationID
				}
				value["acceptedEventId"] = eventID
				value["acceptedSeq"] = seq
				if b, err := json.Marshal(envelope); err == nil {
					body = b
					w.Header().Del("Content-Length")
				}
			}
		}
	}
	if w.status == 0 {
		w.status = http.StatusOK
	}
	w.ResponseWriter.WriteHeader(w.status)
	_, _ = w.ResponseWriter.Write(body)
}

func requestDomain(r *http.Request) string {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/")
	segment, _, _ := strings.Cut(path, "/")
	switch segment {
	case "applications", "application-edit-sessions", "facility-apps":
		return "application"
	case "servers", "credentials":
		return "server"
	case "certificates", "self-signed-certificates", "self-signed-cas":
		return "certificate"
	case "key-assets":
		return "key_asset"
	case "dns":
		return "network"
	default:
		return "system"
	}
}
func (a *App) finishRequest(ctx context.Context, r *http.Request, w *activityResponse, receivedID string) {
	operationID, _, _ := activitylog.ReceiptFromContext(ctx)
	if operationID == "" {
		if logCtx, ok := logging.Lookup(ctx); ok {
			operationID = logCtx.StreamID
		}
	}
	if operationID == "" {
		operationID = receivedID
	}
	status := w.status
	if status == 0 {
		status = http.StatusOK
	}
	fields := []zap.Field{
		zap.String("method", r.Method),
		zap.String("path", r.URL.Path),
		zap.String("route", r.Pattern),
		zap.Int("httpStatus", status),
		zap.String("domain", requestDomain(r)),
		zap.String("causationId", receivedID),
		zap.String("eventType", "request.finished"),
	}
	logger := logging.From(ctx)
	result := "succeeded"
	hadError := false
	if status >= 400 {
		hadError = true
		result = "failed"
		logger.Error("http request finished", fields...)
	} else {
		logger.Info("http request finished", fields...)
	}
	_ = logging.RecordContext(ctx, logging.ContextMeta{
		Origin:   events.OriginUser,
		Title:    r.Method + " " + r.URL.Path,
		Summary:  "request finished",
		Result:   result,
		HadError: hadError,
		Finished: true,
	})
	activitylog.RecordReceipt(ctx, operationID, receivedID, 0)
}
