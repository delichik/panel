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
	panelerr "panel/internal/platform/errors"
	httpx "panel/internal/platform/http"
	id "panel/internal/platform/identity"
	"panel/internal/platform/logging"
)

// systemEventWriter writes synchronously; there is no drop-on-full buffer.
type systemEventWriter struct {
	service *activity.Service
	mu      sync.Mutex
	cancel  context.CancelFunc
	done    chan struct{}
	stop    chan struct{}
	closing bool
	writers sync.WaitGroup
}

func (w *systemEventWriter) Log(ctx context.Context, in runtimeevents.WriteEventInput) {
	w.mu.Lock()
	if w.closing {
		w.mu.Unlock()
		return
	}
	if w.stop == nil {
		w.stop = make(chan struct{})
	}
	stop := w.stop
	w.writers.Add(1)
	w.mu.Unlock()
	defer w.writers.Done()
	eventID := in.ID
	if eventID == "" {
		eventID = id.New("evt")
	}
	event := activity.EventInput{EventID: eventID, EventType: in.EventType, Kind: "observation", Domain: "system", Level: in.Severity, SourceID: in.Source, SourceStreamID: in.SourceModule, Text: in.Summary, OccurredAt: in.OccurredAt, Data: map[string]any{}, Actor: activity.Actor{Kind: "system", ID: in.Source}}
	if in.ResourceID != "" {
		event.Resources = []activity.Resource{{Type: "server", ID: in.ResourceID, Name: in.ResourceName, Role: "target"}}
	}
	for {
		_, err := w.service.Append(ctx, []activity.EventInput{event})
		if err == nil {
			return
		}
		logging.L().Error("Activity persistence unavailable", zap.Error(err))
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case <-time.After(time.Second):
		}
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
				if _, err := w.service.CatchUp(ctx); err != nil && ctx.Err() == nil {
					logging.L().Error("Activity projection failed", zap.Error(err))
				}
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
		ctx := activitylog.WithReceipt(activitylog.WithActor(r.Context(), activitylog.Actor{Kind: "user", ID: "admin", Name: sess.Username}))
		receivedID := ""
		mutation := r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch || r.Method == http.MethodDelete
		if mutation && a.activity != nil {
			if !strings.HasSuffix(r.URL.Path, "/resolve") {
				if err := activitylog.CheckAdmission(ctx, a.store.AppDB()); err != nil {
					httpx.Error(w, err)
					return
				}
			}
			receivedID = id.New("evt")
			receipt, err := a.activity.Append(ctx, []activity.EventInput{{EventID: receivedID, EventType: "request.received", Kind: "request", Domain: requestDomain(r), Action: r.Pattern, SourceStreamID: "http", RequestID: receivedID, Actor: activitylog.ActorFromContext(ctx), Text: r.Method + " " + r.URL.Path, Data: map[string]any{"method": r.Method, "route": r.Pattern}}})
			if err != nil {
				httpx.Error(w, panelerr.New(http.StatusServiceUnavailable, "activity_write_unavailable", "The operation was not started because its audit record could not be saved"))
				return
			}
			_ = receipt
			ctx = activitylog.WithCause(ctx, receivedID)
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
				if db != nil && db.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE execution_id=? AND state='running' AND error_class='uncertainty'`, execution.ExecutionID).Scan(&count) == nil && count > 0 {
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
func (w *activityResponse) finish(ctx context.Context, service *activity.Service) {
	if w.streaming {
		return
	}
	body := w.buffer.Bytes()
	operationID, eventID, seq := activitylog.ReceiptFromContext(ctx)
	if service != nil && eventID != "" {
		if _, err := service.GetEvent(context.WithoutCancel(ctx), eventID); err == nil {
			var envelope map[string]any
			if json.Unmarshal(body, &envelope) == nil {
				key := "data"
				if w.status >= 400 {
					key = "error"
				}
				if value, ok := envelope[key].(map[string]any); ok {
					value["operationId"] = operationID
					value["acceptedEventId"] = eventID
					value["acceptedSeq"] = seq
					if b, err := json.Marshal(envelope); err == nil {
						body = b
						w.Header().Del("Content-Length")
					}
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
	operationID, eventID, _ := activitylog.ReceiptFromContext(ctx)
	if eventID != "" {
		if _, err := a.activity.GetEvent(context.WithoutCancel(ctx), eventID); err != nil {
			operationID = ""
		}
	}
	if operationID == "" && !w.streaming {
		var envelope map[string]any
		if json.Unmarshal(w.buffer.Bytes(), &envelope) == nil {
			key := "data"
			if w.status >= 400 {
				key = "error"
			}
			if value, ok := envelope[key].(map[string]any); ok {
				if id, ok := value["operationId"].(string); ok && id != "" {
					if event, err := a.activity.OperationReceipt(context.WithoutCancel(ctx), id); err == nil {
						operationID = id
						eventID = event.EventID
						activitylog.RecordReceipt(ctx, id, event.EventID, event.Seq)
					}
				}
			}
		}
	}
	independent := operationID == ""
	if independent {
		operationID = id.New("op")
	}
	status := w.status
	if status == 0 {
		status = http.StatusOK
	}
	data := map[string]any{"method": r.Method, "route": r.Pattern, "httpStatus": status}
	eventType := "request.finished"
	level := "info"
	if status >= 400 {
		level = "error"
	}
	if independent {
		eventType = "operation.finished"
		data["phase"] = "ended"
		data["result"] = "succeeded"
		if status >= 400 {
			data["result"] = "failed"
		}
	}
	receipt, err := a.activity.Append(context.WithoutCancel(ctx), []activity.EventInput{{EventType: eventType, Kind: "request", Level: level, Domain: requestDomain(r), Action: r.Pattern, OperationID: operationID, CausationEventID: receivedID, RequestID: receivedID, SourceStreamID: "http", Actor: activitylog.ActorFromContext(ctx), Text: r.Method + " " + r.URL.Path, Data: data}})
	if err != nil {
		logging.L().Error("Business response could not be durably recorded", zap.String("request_event_id", receivedID), zap.Error(err))
		if !w.streaming {
			w.buffer.Reset()
			w.status = 0
			httpx.Error(w, panelerr.WithDetails(panelerr.New(http.StatusServiceUnavailable, "activity_result_unrecorded", "The operation may have completed, but its result could not be recorded; verify the resource before retrying"), map[string]any{"eventId": receivedID}))
		}
		return
	}
	if independent && len(receipt.Events) > 0 {
		activitylog.RecordReceipt(ctx, operationID, receipt.Events[0].EventID, receipt.Events[0].Seq)
	}
}
