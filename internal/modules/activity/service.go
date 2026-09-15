package activity

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"panel/internal/platform/activitylog"
)

type Service struct {
	db, index *sql.DB
	mu        sync.Mutex
	Commands  func(context.Context, []Execution) []Command
}

func NewService(db *sql.DB, index ...*sql.DB) *Service {
	dst := db
	if len(index) > 0 && index[0] != nil {
		dst = index[0]
	}
	return &Service{db: db, index: dst}
}
func (s *Service) Append(ctx context.Context, events []EventInput) (Receipt, error) {
	return activitylog.Append(ctx, s.db, events)
}
func (s *Service) AppendTx(ctx context.Context, tx *sql.Tx, events []EventInput) (Receipt, error) {
	return activitylog.AppendTx(ctx, tx, events)
}
func (s *Service) Init(ctx context.Context) error {
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS activity_projection_events(seq INTEGER PRIMARY KEY,event_id TEXT NOT NULL UNIQUE,operation_id TEXT NOT NULL,execution_id TEXT NOT NULL,step_id TEXT NOT NULL,domain TEXT NOT NULL,action TEXT NOT NULL,kind TEXT NOT NULL,level TEXT NOT NULL,trigger_type TEXT NOT NULL,actor_id TEXT NOT NULL,recorded_at TEXT NOT NULL,text TEXT NOT NULL,resources_json TEXT NOT NULL,body_json TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS activity_projection_operation ON activity_projection_events(operation_id,seq)`,
		`CREATE INDEX IF NOT EXISTS activity_projection_execution ON activity_projection_events(execution_id,seq)`,
		`CREATE TABLE IF NOT EXISTS activity_operation_versions(operation_id TEXT NOT NULL,applied_seq INTEGER NOT NULL,body_json TEXT NOT NULL,state_json TEXT NOT NULL,PRIMARY KEY(operation_id,applied_seq))`,
		`CREATE TABLE IF NOT EXISTS activity_projection_checkpoint(id INTEGER PRIMARY KEY CHECK(id=1),seq INTEGER NOT NULL,version INTEGER NOT NULL)`,
		`INSERT OR IGNORE INTO activity_projection_checkpoint(id,seq,version) VALUES(1,0,4)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS activity_search USING fts5(text, tokenize='trigram')`,
	} {
		if _, err := s.index.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	var version int
	if err := s.index.QueryRowContext(ctx, `SELECT version FROM activity_projection_checkpoint WHERE id=1`).Scan(&version); err != nil {
		return err
	}
	if version != 4 {
		tx, err := s.index.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		for _, q := range []string{`DELETE FROM activity_projection_events`, `DELETE FROM activity_operation_versions`, `DELETE FROM activity_search`, `UPDATE activity_projection_checkpoint SET seq=0,version=4 WHERE id=1`} {
			if _, err = tx.ExecContext(ctx, q); err != nil {
				return err
			}
		}
		return tx.Commit()
	}
	return nil
}
func (s *Service) Head(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0) FROM activity_events`).Scan(&n)
	return n, err
}
func (s *Service) CatchUp(ctx context.Context) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var watermark int64
	if err := s.index.QueryRowContext(ctx, `SELECT seq FROM activity_projection_checkpoint WHERE id=1`).Scan(&watermark); err != nil {
		return 0, err
	}
	head, err := s.Head(ctx)
	if err != nil {
		return watermark, err
	}
	// Bound each pass; callers receive the committed projection waterline.
	for pass := 0; pass < 20 && watermark < head; pass++ {
		rows, err := s.db.QueryContext(ctx, `SELECT `+activitylog.Columns+` FROM activity_events WHERE seq>? AND seq<=? ORDER BY seq LIMIT 500`, watermark, head)
		if err != nil {
			return watermark, err
		}
		batch := []Event{}
		for rows.Next() {
			e, err := activitylog.Scan(rows)
			if err != nil {
				rows.Close()
				return watermark, err
			}
			batch = append(batch, e)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return watermark, err
		}
		if len(batch) == 0 {
			break
		}
		tx, err := s.index.BeginTx(ctx, nil)
		if err != nil {
			return watermark, err
		}
		if err = s.projectBatch(ctx, tx, batch); err != nil {
			tx.Rollback()
			return watermark, err
		}
		next := batch[len(batch)-1].Seq
		if _, err = tx.ExecContext(ctx, `UPDATE activity_projection_checkpoint SET seq=? WHERE id=1`, next); err != nil {
			tx.Rollback()
			return watermark, err
		}
		if err = tx.Commit(); err != nil {
			return watermark, err
		}
		watermark = next
	}
	return watermark, nil
}

type operationState struct {
	Operation  Operation            `json:"operation"`
	Executions map[string]Execution `json:"executions"`
	Steps      map[string]Step      `json:"steps"`
	Related    []string             `json:"related"`
	Gaps       map[string]bool      `json:"gaps"`
	Streams    map[string]bool      `json:"streams"`
}

func newState(id string) operationState {
	return operationState{Operation: Operation{OperationID: id, Resources: []Resource{}, EvidenceComplete: true}, Executions: map[string]Execution{}, Steps: map[string]Step{}, Related: []string{}, Gaps: map[string]bool{}, Streams: map[string]bool{}}
}
func (s *Service) projectBatch(ctx context.Context, tx *sql.Tx, batch []Event) error {
	states := map[string]operationState{}
	for _, e := range batch {
		body, err := json.Marshal(e)
		if err != nil {
			return err
		}
		resources, _ := json.Marshal(e.Resources)
		_, err = tx.ExecContext(ctx, `INSERT INTO activity_projection_events(seq,event_id,operation_id,execution_id,step_id,domain,action,kind,level,trigger_type,actor_id,recorded_at,text,resources_json,body_json) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, e.Seq, e.EventID, e.OperationID, e.ExecutionID, e.StepID, e.Domain, e.Action, e.Kind, e.Level, e.Trigger, e.Actor.ID, e.RecordedAt.UTC().Format("2006-01-02T15:04:05.000000000Z"), e.Text, string(resources), string(body))
		if err != nil {
			return err
		}
		searchText := strings.Join([]string{e.Text, e.EventType, e.Action, e.EventID, e.OperationID, string(resources), textData(e, "error"), textData(e, "detail"), textData(e, "errorCode")}, " ")
		_, err = tx.ExecContext(ctx, `INSERT INTO activity_search(rowid,text) VALUES(?,?)`, e.Seq, searchText)
		if err != nil {
			return err
		}
		if e.OperationID == "" {
			continue
		}
		state, ok := states[e.OperationID]
		if !ok {
			var raw string
			err = tx.QueryRowContext(ctx, `SELECT state_json FROM activity_operation_versions WHERE operation_id=? ORDER BY applied_seq DESC LIMIT 1`, e.OperationID).Scan(&raw)
			if errors.Is(err, sql.ErrNoRows) {
				state = newState(e.OperationID)
			} else if err != nil {
				return err
			} else if err = json.Unmarshal([]byte(raw), &state); err != nil {
				return err
			}
		}
		firstEvent := state.Operation.FirstSeq == 0
		beforeHadError, beforeComplete := state.Operation.HadError, state.Operation.EvidenceComplete
		applyEvent(&state, e)
		states[e.OperationID] = state
		if e.Kind == "output" && !firstEvent && state.Operation.HadError == beforeHadError && state.Operation.EvidenceComplete == beforeComplete {
			continue
		}
		opJSON, _ := json.Marshal(state.Operation)
		stateJSON, _ := json.Marshal(state)
		if _, err = tx.ExecContext(ctx, `INSERT INTO activity_operation_versions(operation_id,applied_seq,body_json,state_json) VALUES(?,?,?,?)`, e.OperationID, e.Seq, string(opJSON), string(stateJSON)); err != nil {
			return err
		}
	}
	return nil
}
func textData(e Event, key string) string {
	v := e.Data[key]
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
func failureSummary(e Event) string {
	for _, key := range []string{"error", "detail", "errorCode"} {
		if value := strings.TrimSpace(textData(e, key)); value != "" {
			return value
		}
	}
	return ""
}
func phaseResult(e Event) (string, string) {
	p, r := textData(e, "phase"), ""
	if value, ok := e.Data["result"].(string); ok {
		r = value
	}
	if r == "success" {
		r = "succeeded"
	}
	if p != "" {
		return p, r
	}
	status := textData(e, "status")
	if status == "" {
		status = textData(e, "state")
	}
	switch status {
	case "queued", "scheduled", "pending":
		return "queued", ""
	case "running":
		return "running", ""
	case "failed_retryable", "blocked":
		return "waiting", ""
	case "completed", "succeeded":
		return "ended", "succeeded"
	case "failed":
		return "ended", "failed"
	case "cancelled":
		return "ended", "cancelled"
	}
	switch {
	case strings.HasSuffix(e.EventType, ".started"):
		return "running", ""
	case strings.HasSuffix(e.EventType, ".requested"), strings.HasSuffix(e.EventType, ".queued"):
		return "queued", ""
	case strings.HasSuffix(e.EventType, ".completed"), strings.HasSuffix(e.EventType, ".succeeded"):
		return "ended", "succeeded"
	case strings.HasSuffix(e.EventType, ".failed"):
		return "ended", "failed"
	case strings.HasSuffix(e.EventType, ".cancelled"):
		return "ended", "cancelled"
	case e.EventType == "intent.superseded":
		return "ended", "superseded"
	case e.EventType == "uncertainty.detected":
		return "waiting", ""
	}
	return "", ""
}
func applyEvent(s *operationState, e Event) {
	o := &s.Operation
	if s.Streams == nil {
		s.Streams = map[string]bool{}
	}
	if strings.HasPrefix(e.SourceID, "agent:") {
		key := e.SourceID + ":" + e.SourceEpoch + ":" + e.SourceStreamID
		if _, ok := s.Streams[key]; !ok {
			s.Streams[key] = false
		}
		if e.EventType == "stream.closed" {
			s.Streams[key] = true
		}
	}
	if o.FirstSeq == 0 {
		o.FirstSeq = e.Seq
		o.CreatedAt = e.OccurredAt
		o.Title = e.Text
		o.Domain = e.Domain
		o.Action = e.Action
		o.Trigger = e.Trigger
		o.Actor = e.Actor
		if e.Initiator.Kind != "" {
			o.Actor = e.Initiator
		}
		o.Phase = "queued"
	}
	o.LastSeq = e.Seq
	o.UpdatedAt = e.RecordedAt
	o.EventCount++
	if o.Title == "" {
		o.Title = e.Action
	}
	if o.Action == "" {
		o.Action = e.Action
	}
	for _, r := range e.Resources {
		found := false
		for _, old := range o.Resources {
			if old.ID == r.ID && old.Type == r.Type {
				found = true
				break
			}
		}
		if !found {
			o.Resources = append(o.Resources, r)
		}
	}
	if e.Level == "error" {
		o.HadError = true
		if summary := failureSummary(e); summary != "" {
			o.FailureSummary = summary
		}
	}
	p, r := phaseResult(e)
	if e.EventType == "uncertainty.detected" || textData(e, "errorClass") == "uncertainty" || textData(e, "uncertainty") == "true" || textData(e, "uncertainty") == "1" {
		o.Uncertainty = true
	}
	if e.EventType == "evidence.gap_detected" {
		s.Gaps[textData(e, "gapId")] = true
	}
	if e.EventType == "evidence.gap_resolved" {
		delete(s.Gaps, textData(e, "gapId"))
	}
	if e.StepID != "" {
		st := s.Steps[e.StepID]
		st.StepID = e.StepID
		st.ExecutionID = e.ExecutionID
		st.ParentStepID = e.ParentStepID
		st.Name = textData(e, "step")
		if st.Name == "" {
			st.Name = textData(e, "stage")
		}
		if st.Name == "" {
			st.Name = e.StepID
		}
		if p != "" {
			st.Phase = p
			st.Result = r
			if p == "running" && st.StartedAt == nil {
				t := e.OccurredAt
				st.StartedAt = &t
			}
			if p == "ended" {
				t := e.OccurredAt
				st.FinishedAt = &t
			}
		}
		s.Steps[e.StepID] = st
	} else if e.ExecutionID != "" && p != "" {
		ex := s.Executions[e.ExecutionID]
		ex.ExecutionID = e.ExecutionID
		ex.LastSeq = e.Seq
		logical := textData(e, "logicalExecutionId")
		if logical == "" {
			logical = textData(e, "jobId")
		}
		if logical == "" {
			logical = textData(e, "taskId")
		}
		if logical == "" {
			logical = e.ExecutionID
		}
		ex.LogicalExecutionID = logical
		if textData(e, "isAggregate") == "true" || textData(e, "isAggregate") == "1" {
			ex.IsAggregate = true
		}
		ex.RunID = e.RunID
		ex.Resources = e.Resources
		ex.Phase = p
		ex.Result = r
		if p == "running" && ex.StartedAt == nil {
			t := e.OccurredAt
			ex.StartedAt = &t
		}
		if p == "ended" {
			t := e.OccurredAt
			ex.FinishedAt = &t
		}
		s.Executions[e.ExecutionID] = ex
	}
	if p != "" && e.StepID == "" {
		o.Phase = p
		o.Result = r
		if p == "running" && o.StartedAt == nil {
			t := e.OccurredAt
			o.StartedAt = &t
		}
		if p == "ended" {
			t := e.OccurredAt
			o.FinishedAt = &t
		} else {
			o.FinishedAt = nil
		}
	}
	if id := textData(e, "relatedOperationId"); id != "" {
		s.Related = append(s.Related, id)
	}
	if id := textData(e, "supersededBy"); id != "" {
		s.Related = append(s.Related, id)
	}
	o.AttemptCount = len(s.Executions)
	// Results aggregate independently retained executions; a task's latest execution is represented by its execution identity.
	if len(s.Executions) > 0 && o.Result != "superseded" {
		active, success, failed, cancelled, unknown := false, false, false, false, false
		latest := map[string]Execution{}
		for _, ex := range s.Executions {
			if ex.IsAggregate {
				continue
			}
			key := ex.LogicalExecutionID
			if key == "" {
				key = ex.ExecutionID
			}
			prev, ok := latest[key]
			if !ok || ex.LastSeq > prev.LastSeq {
				latest[key] = ex
			}
		}
		if len(latest) == 0 {
			for key, ex := range s.Executions {
				latest[key] = ex
			}
		}
		for _, ex := range latest {
			switch {
			case ex.Phase != "ended":
				active = true
			case ex.Result == "succeeded":
				success = true
			case ex.Result == "cancelled":
				cancelled = true
			case ex.Result == "unknown":
				unknown = true
			default:
				failed = true
			}
		}
		if active {
			if o.Phase == "ended" {
				o.Phase = "running"
			}
			o.Result = ""
			o.FinishedAt = nil
		} else {
			o.Phase = "ended"
			switch {
			case unknown:
				o.Result = "unknown"
			case success && (failed || cancelled):
				o.Result = "partial"
			case failed:
				o.Result = "failed"
			case cancelled:
				o.Result = "cancelled"
			default:
				o.Result = "succeeded"
			}
		}
	}
	if o.Phase == "ended" && o.Result == "succeeded" {
		o.Uncertainty = false
		o.FailureSummary = ""
	}
	o.EvidenceComplete = len(s.Gaps) == 0
	for _, closed := range s.Streams {
		if !closed {
			o.EvidenceComplete = false
		}
	}
	o.Attention = o.Result == "failed" || o.Result == "partial" || o.Result == "unknown" || o.Uncertainty || !o.EvidenceComplete
}
func executionTime(e Execution) time.Time {
	if e.StartedAt != nil {
		return *e.StartedAt
	}
	if e.FinishedAt != nil {
		return *e.FinishedAt
	}
	return time.Time{}
}
