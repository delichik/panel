// Package executionevents owns the Agent's durable transport copies. Only a
// Panel acknowledgement permits their reclamation; execution identity/result
// tombstones survive ACK to prevent duplicate remote mutations.
package executionevents

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
	contract "panel/internal/agent/contract"
)

var ErrConflict = errors.New("execution identity conflicts with a previously accepted request")
var ErrInFlight = errors.New("a previous execution for this instance has no durable result; reconciliation is required")
var ErrUnknown = errors.New("execution result is unknown; repeating the side effect is prohibited")
var ErrNotFound = errors.New("execution not found")

// Store is private to one Agent process. SQLite also protects the durable
// instance exclusion across accidentally overlapping Agent processes.
type Store struct {
	db                       *sql.DB
	sourceID, epoch, session string
	mu                       sync.Mutex
	active                   map[string]bool
}

type Session struct {
	store   *Store
	request contract.RuntimeReconcileRequest
	secrets []string
}

func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "events.db")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	fail := func(err error) (*Store, error) { db.Close(); return nil, err }
	for _, statement := range []string{
		`PRAGMA journal_mode=WAL`, `PRAGMA synchronous=FULL`, `PRAGMA busy_timeout=5000`,
		`CREATE TABLE IF NOT EXISTS identity (id INTEGER PRIMARY KEY CHECK(id=1), source_id TEXT NOT NULL, epoch TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS executions (id TEXT PRIMARY KEY, request_hash TEXT NOT NULL, request_json TEXT NOT NULL, instance_id TEXT NOT NULL, session TEXT NOT NULL, state TEXT NOT NULL, head_seq INTEGER NOT NULL DEFAULT 0, end_seq INTEGER NOT NULL DEFAULT 0, ack_seq INTEGER NOT NULL DEFAULT 0, result_json TEXT, error TEXT NOT NULL DEFAULT '')`,
		`CREATE UNIQUE INDEX IF NOT EXISTS active_instance ON executions(instance_id) WHERE state IN ('running','unknown')`,
		`CREATE TABLE IF NOT EXISTS events (execution_id TEXT NOT NULL, source_seq INTEGER NOT NULL, body TEXT NOT NULL, PRIMARY KEY(execution_id, source_seq))`,
		`CREATE TABLE IF NOT EXISTS manual_resolutions (execution_id TEXT PRIMARY KEY, request_hash TEXT NOT NULL, declaration_json TEXT NOT NULL, occurred_at TEXT NOT NULL)`,
	} {
		if _, err = db.Exec(statement); err != nil {
			return fail(err)
		}
	}
	s := &Store{db: db, session: newID(), active: map[string]bool{}}
	err = db.QueryRow(`SELECT source_id,epoch FROM identity WHERE id=1`).Scan(&s.sourceID, &s.epoch)
	if errors.Is(err, sql.ErrNoRows) {
		s.sourceID, s.epoch = "agent:"+newID(), newID()
		_, err = db.Exec(`INSERT INTO identity(id,source_id,epoch) VALUES(1,?,?)`, s.sourceID, s.epoch)
	}
	if err != nil {
		return fail(err)
	}
	return s, nil
}
func (s *Store) Close() error { return s.db.Close() }

// Begin commits the invocation identity and first fact before callers may touch
// Docker. Repeated requests return their durable result or fail closed.
func (s *Store) Begin(ctx context.Context, req contract.RuntimeReconcileRequest) (*Session, *contract.ExecutionResult, error) {
	if req.ExecutionID == "" || req.OperationID == "" || req.RunID == "" || req.InstanceID == "" {
		return nil, nil, errors.New("operation, run, execution and instance identities are required")
	}
	raw, err := json.Marshal(req)
	if err != nil {
		return nil, nil, err
	}
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	// A write acquires SQLite's writer lock before reading identities.
	if _, err = tx.ExecContext(ctx, `UPDATE identity SET id=id WHERE id=1`); err != nil {
		return nil, nil, err
	}
	var oldHash string
	err = tx.QueryRowContext(ctx, `SELECT request_hash FROM executions WHERE id=?`, req.ExecutionID).Scan(&oldHash)
	if err == nil {
		if oldHash != hash {
			return nil, nil, ErrConflict
		}
		var result contract.ExecutionResult
		result, err = s.resultTx(ctx, tx, req.ExecutionID)
		return nil, &result, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, nil, err
	}
	var pending int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM executions WHERE instance_id=? AND state IN ('running','unknown')`, req.InstanceID).Scan(&pending); err != nil {
		return nil, nil, err
	}
	if pending > 0 {
		return nil, nil, ErrInFlight
	}
	safe := req
	safe.Spec = contract.RuntimeReconcileRequest{}.Spec
	safe.Spec.ContainerName = req.Spec.ContainerName
	safeJSON, _ := json.Marshal(safe)
	if _, err = tx.ExecContext(ctx, `INSERT INTO executions(id,request_hash,request_json,instance_id,session,state) VALUES(?,?,?,?,?,'running')`, req.ExecutionID, hash, string(safeJSON), req.InstanceID, s.session); err != nil {
		return nil, nil, err
	}
	session := &Session{store: s, request: req, secrets: requestSecrets(req)}
	data, _ := json.Marshal(map[string]any{"jobId": req.JobID, "applicationId": req.ApplicationID, "instanceId": req.InstanceID, "serverId": req.ServerID, "action": req.Action, "generation": req.DesiredGeneration, "specHash": req.DesiredSpecHash, "revisionId": req.DesiredRevisionID})
	if err = session.appendTx(ctx, tx, contract.ExecutionEvent{EventType: "execution.started", Status: "running", DataJSON: string(data)}); err != nil {
		return nil, nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, nil, err
	}
	s.active[req.ExecutionID] = true
	return session, nil, nil
}

// Abandon removes the in-process execution claim without changing durable
// history. An unfinished stream then reports unknown and keeps its fence.
func (s *Session) Abandon() {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	delete(s.store.active, s.request.ExecutionID)
}

func (s *Session) Append(ctx context.Context, event contract.ExecutionEvent) error {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if event.EventType == "output.chunk" {
		// Redact the complete incoming value before splitting, so a secret spanning
		// a chunk boundary cannot escape replacement.
		runes := []rune(s.Redact(event.Text))
		lineID := newID()
		part := 0
		if len(runes) == 0 {
			return tx.Commit()
		}
		for len(runes) > 0 {
			n := len(runes)
			if n > 4096 {
				n = 4096
			}
			chunk := event
			chunk.Text = string(runes[:n])
			data, _ := json.Marshal(map[string]any{"lineId": lineID, "partIndex": part})
			chunk.DataJSON = string(data)
			if err = s.appendTx(ctx, tx, chunk); err != nil {
				return err
			}
			runes = runes[n:]
			part++
		}
	} else if err = s.appendTx(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Session) appendTx(ctx context.Context, tx *sql.Tx, event contract.ExecutionEvent) error {
	var seq int64
	var state string
	if err := tx.QueryRowContext(ctx, `SELECT head_seq,state FROM executions WHERE id=?`, s.request.ExecutionID).Scan(&seq, &state); err != nil {
		return err
	}
	if state != "running" && state != "unknown" {
		return errors.New("execution stream is closed")
	}
	event.SourceSeq = seq + 1
	event.EventID = s.store.epoch + ":" + s.request.ExecutionID + ":" + fmt.Sprint(event.SourceSeq)
	event.OperationID = s.request.OperationID
	event.RunID = s.request.RunID
	event.ExecutionID = s.request.ExecutionID
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	event.Text = s.Redact(event.Text)
	if event.DataJSON == "" {
		event.DataJSON = "{}"
	}
	if !json.Valid([]byte(event.DataJSON)) {
		return errors.New("invalid execution event data")
	}
	// Data is a typed allow-list supplied by the recorder, never the runtime spec.
	body, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO events(execution_id,source_seq,body) VALUES(?,?,?)`, event.ExecutionID, event.SourceSeq, string(body)); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE executions SET head_seq=? WHERE id=?`, event.SourceSeq, event.ExecutionID)
	return err
}

// Finish saves the exact result and the final sequence in one durable commit.
// Even after ACK the result remains available for Panel uncertainty recovery.
func (s *Session) Finish(ctx context.Context, result contract.RuntimeReconcileResponse, runErr error) error {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result.ErrorMessage = s.Redact(result.ErrorMessage)
	result.ErrorDetail = s.Redact(result.ErrorDetail)
	for i := range result.Steps {
		result.Steps[i].Detail = s.Redact(result.Steps[i].Detail)
	}
	message := ""
	outcome := "succeeded"
	state := "finished"
	if runErr != nil {
		message = s.Redact(runErr.Error())
	}
	if runErr != nil || result.ErrorCode != "" {
		outcome = "failed"
	}
	var networkError net.Error
	var persistenceError *contract.EventPersistenceError
	if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) || errors.As(runErr, &networkError) || errors.As(runErr, &persistenceError) {
		outcome = "unknown"
		state = "unknown"
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	data, _ := json.Marshal(map[string]any{"result": result, "transportError": message})
	eventType := "execution.finished"
	if state == "unknown" {
		eventType = "uncertainty.detected"
	}
	if err = s.appendTx(ctx, tx, contract.ExecutionEvent{EventType: eventType, Status: outcome, Text: message, DataJSON: string(data)}); err != nil {
		return err
	}
	if state == "unknown" {
		if _, err = tx.ExecContext(ctx, `UPDATE executions SET state='unknown',result_json=?,error=? WHERE id=?`, string(raw), message, s.request.ExecutionID); err != nil {
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		delete(s.store.active, s.request.ExecutionID)
		return nil
	}
	var head int64
	if err = tx.QueryRowContext(ctx, `SELECT head_seq FROM executions WHERE id=?`, s.request.ExecutionID).Scan(&head); err != nil {
		return err
	}
	closed, _ := json.Marshal(map[string]any{"endSeq": head + 1})
	if err = s.appendTx(ctx, tx, contract.ExecutionEvent{EventType: "stream.closed", DataJSON: string(closed)}); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE executions SET state=?,end_seq=head_seq,result_json=?,error=? WHERE id=?`, state, string(raw), message, s.request.ExecutionID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	delete(s.store.active, s.request.ExecutionID)
	return nil
}

func (s *Store) Read(ctx context.Context, req contract.ExecutionEventsRequest) (contract.ExecutionEventsResponse, error) {
	out := contract.ExecutionEventsResponse{SourceID: s.sourceID, SourceEpoch: s.epoch, SourceStreamID: req.ExecutionID, Events: []contract.ExecutionEvent{}}
	if req.AfterSourceSeq < 0 || req.Limit < 0 || req.Limit > 500 {
		return out, errors.New("invalid event cursor or limit")
	}
	if req.SourceEpoch != "" && req.SourceEpoch != s.epoch || req.SourceStreamID != "" && req.SourceStreamID != req.ExecutionID {
		return out, ErrConflict
	}
	limit := req.Limit
	if limit == 0 {
		limit = 100
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.db.QueryRowContext(ctx, `SELECT head_seq,end_seq,ack_seq FROM executions WHERE id=?`, req.ExecutionID).Scan(&out.HeadSeq, &out.EndSeq, &out.AckedThroughSeq); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return out, ErrNotFound
		}
		return out, err
	}
	if req.AfterSourceSeq > out.HeadSeq {
		return out, errors.New("event cursor exceeds source head")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT body FROM events WHERE execution_id=? AND source_seq>? ORDER BY source_seq LIMIT ?`, req.ExecutionID, req.AfterSourceSeq, limit)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	last := req.AfterSourceSeq
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return out, err
		}
		var event contract.ExecutionEvent
		if err = json.Unmarshal([]byte(raw), &event); err != nil {
			return out, err
		}
		out.Events = append(out.Events, event)
		last = event.SourceSeq
	}
	out.HasMore = last < out.HeadSeq && last >= out.AckedThroughSeq
	return out, rows.Err()
}
func (s *Store) Ack(ctx context.Context, ack contract.ExecutionEventsAck) error {
	if ack.SourceID != s.sourceID || ack.SourceEpoch != s.epoch || ack.SourceStreamID != ack.ExecutionID || ack.ThroughSourceSeq < 0 {
		return ErrConflict
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var head, previous int64
	if err = tx.QueryRowContext(ctx, `SELECT head_seq,ack_seq FROM executions WHERE id=?`, ack.ExecutionID).Scan(&head, &previous); err != nil {
		return err
	}
	if ack.ThroughSourceSeq > head {
		return errors.New("acknowledgement exceeds source head")
	}
	if ack.ThroughSourceSeq <= previous {
		return nil
	}
	if _, err = tx.ExecContext(ctx, `UPDATE executions SET ack_seq=? WHERE id=?`, ack.ThroughSourceSeq, ack.ExecutionID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM events WHERE execution_id=? AND source_seq<=?`, ack.ExecutionID, ack.ThroughSourceSeq); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) Result(ctx context.Context, id string) (contract.ExecutionResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return contract.ExecutionResult{}, err
	}
	defer tx.Rollback()
	return s.resultTx(ctx, tx, id)
}
func (s *Store) resultTx(ctx context.Context, tx *sql.Tx, id string) (contract.ExecutionResult, error) {
	var out contract.ExecutionResult
	var raw sql.NullString
	var session string
	err := tx.QueryRowContext(ctx, `SELECT state,result_json,error,end_seq,session FROM executions WHERE id=?`, id).Scan(&out.State, &raw, &out.Error, &out.EndSeq, &session)
	if errors.Is(err, sql.ErrNoRows) {
		out.State = "missing"
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if out.State == "running" && (session != s.session || !s.active[id]) {
		out.State = "unknown"
	}
	if raw.Valid {
		out.Result = &contract.RuntimeReconcileResponse{}
		err = json.Unmarshal([]byte(raw.String), out.Result)
	}
	return out, err
}
func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

var credentials = regexp.MustCompile(`(?i)(password|passwd|passphrase|privatekey|token|authorization|secret)([=:][ \t]*)([^\s,;]+)`)
var bearer = regexp.MustCompile(`(?i)bearer\s+[^\s,;]+`)

func (s *Session) Redact(text string) string {
	for _, secret := range s.secrets {
		text = strings.ReplaceAll(text, secret, "[REDACTED]")
	}
	text = credentials.ReplaceAllString(text, "${1}${2}[REDACTED]")
	return bearer.ReplaceAllString(text, "Bearer [REDACTED]")
}
func requestSecrets(req contract.RuntimeReconcileRequest) []string {
	var secrets []string
	for _, v := range req.Spec.Env {
		if v != "" {
			secrets = append(secrets, v)
		}
	}
	for _, v := range req.Spec.Files {
		if len(v.Content) > 0 {
			secrets = append(secrets, string(v.Content))
		}
	}
	sort.Slice(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
	return secrets
}

// RecoverySession exposes only the immutable safe identity and expected version.
// It never contains environment variables, files, commands or registry secrets.
func (s *Store) RecoverySession(ctx context.Context, id string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active[id] {
		return nil, nil
	}
	var raw, state string
	var end int64
	if err := s.db.QueryRowContext(ctx, `SELECT request_json,state,end_seq FROM executions WHERE id=?`, id).Scan(&raw, &state, &end); err != nil {
		return nil, err
	}
	if state == "finished" || end != 0 {
		return nil, nil
	}
	var req contract.RuntimeReconcileRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		return nil, err
	}
	return &Session{store: s, request: req}, nil
}
func (s *Session) Request() contract.RuntimeReconcileRequest { return s.request }
