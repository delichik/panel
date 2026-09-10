package activity

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"panel/internal/platform/activitylog"
	panelerr "panel/internal/platform/errors"
)

type cursor struct {
	Snapshot    int64  `json:"s"`
	After       int64  `json:"a"`
	ID          string `json:"i,omitempty"`
	Fingerprint string `json:"f"`
}

func fingerprint(f Filter) string {
	f.Cursor = ""
	f.SnapshotSeq = 0
	f.AfterSeq = 0
	b, _ := json.Marshal(f)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func encodeCursor(c cursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}
func (s *Service) bounds(ctx context.Context, f Filter) (cursor, int64, int64, error) {
	mark, err := s.CatchUp(ctx)
	if err != nil {
		return cursor{}, 0, 0, err
	}
	head, err := s.Head(ctx)
	if err != nil {
		return cursor{}, 0, 0, err
	}
	c := cursor{Snapshot: mark, Fingerprint: fingerprint(f)}
	if f.SnapshotSeq > 0 {
		if f.SnapshotSeq > mark {
			return c, head, mark, panelerr.Conflict("activity_index_pending", "The requested records are saved but not indexed yet")
		}
		c.Snapshot = f.SnapshotSeq
	}
	if f.Cursor != "" {
		b, err := base64.RawURLEncoding.DecodeString(f.Cursor)
		if err != nil || json.Unmarshal(b, &c) != nil || c.Fingerprint != fingerprint(f) || c.Snapshot > mark || c.Snapshot < 0 {
			return cursor{}, head, mark, panelerr.BadRequest("activity_cursor_invalid", "The cursor does not match this query")
		}
	}
	return c, head, mark, nil
}
func limit(n int) int {
	if n <= 0 {
		return 100
	}
	if n > 500 {
		return 500
	}
	return n
}
func filterSQL(f Filter) (string, []any) {
	parts := []string{"1=1"}
	args := []any{}
	for _, x := range []struct{ column, value string }{{"domain", f.Domain}, {"action", f.Action}, {"kind", f.Kind}, {"level", f.Level}, {"trigger_type", f.Trigger}, {"actor_id", f.ActorID}, {"operation_id", f.OperationID}, {"step_id", f.StepID}} {
		if x.value == "" {
			continue
		}
		values := strings.Split(x.value, ",")
		parts = append(parts, x.column+" IN ("+strings.TrimSuffix(strings.Repeat("?,", len(values)), ",")+")")
		for _, v := range values {
			args = append(args, strings.TrimSpace(v))
		}
	}
	if f.ExecutionID != "" {
		parts = append(parts, "(execution_id=? OR json_extract(body_json,'$.data.taskId')=?)")
		args = append(args, f.ExecutionID, f.ExecutionID)
	}
	if f.From != nil {
		parts = append(parts, "recorded_at>=?")
		args = append(args, f.From.UTC().Format("2006-01-02T15:04:05.000000000Z"))
	}
	if f.To != nil {
		parts = append(parts, "recorded_at<=?")
		args = append(args, f.To.UTC().Format("2006-01-02T15:04:05.000000000Z"))
	}
	if f.ResourceID != "" || f.ResourceType != "" {
		expr := "EXISTS(SELECT 1 FROM json_each(resources_json) r WHERE 1=1"
		if f.ResourceID != "" {
			expr += " AND json_extract(r.value,'$.resourceId')=?"
			args = append(args, f.ResourceID)
		}
		if f.ResourceType != "" {
			expr += " AND json_extract(r.value,'$.resourceType')=?"
			args = append(args, f.ResourceType)
		}
		parts = append(parts, expr+")")
	}
	if f.Q != "" {
		if len([]rune(f.Q)) >= 3 {
			parts = append(parts, `seq IN (SELECT rowid FROM activity_search WHERE activity_search MATCH ?)`)
			args = append(args, `"`+strings.ReplaceAll(f.Q, `"`, `""`)+`"`)
		} else {
			parts = append(parts, `(text LIKE ? ESCAPE '\' OR resources_json LIKE ? ESCAPE '\' OR event_id=?)`)
			q := "%" + strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(f.Q) + "%"
			args = append(args, q, q, f.Q)
		}
	}
	return strings.Join(parts, " AND "), args
}
func (s *Service) Events(ctx context.Context, f Filter) (Page[Event], error) {
	return s.events(ctx, f, false)
}
func (s *Service) Tail(ctx context.Context, f Filter) (Page[Event], error) {
	return s.events(ctx, f, true)
}
func (s *Service) events(ctx context.Context, f Filter, tail bool) (Page[Event], error) {
	f.Limit = limit(f.Limit)
	c, head, mark, err := s.bounds(ctx, f)
	page := Page[Event]{Items: []Event{}, SnapshotSeq: c.Snapshot, HeadSeq: head, ProjectedThroughSeq: mark, IndexState: "ready"}
	if mark < head {
		page.IndexState = "catching_up"
	}
	if err != nil {
		return page, err
	}
	where, args := filterSQL(f)
	where += " AND seq<=?"
	args = append(args, c.Snapshot)
	countWhere, countArgs := where, append([]any{}, args...)
	direction := "DESC"
	if tail {
		direction = "ASC"
		where += " AND seq>?"
		after := f.AfterSeq
		if c.After > after {
			after = c.After
		}
		args = append(args, after)
	} else if c.After > 0 {
		where += " AND seq<?"
		args = append(args, c.After)
	}
	args = append(args, f.Limit+1)
	rows, err := s.index.QueryContext(ctx, "SELECT body_json FROM activity_projection_events WHERE "+where+" ORDER BY seq "+direction+" LIMIT ?", args...)
	if err != nil {
		return page, err
	}
	for rows.Next() {
		var raw []byte
		var e Event
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return page, err
		}
		if err = json.Unmarshal(raw, &e); err != nil {
			rows.Close()
			return page, err
		}
		page.Items = append(page.Items, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return page, err
	}
	if len(page.Items) > f.Limit {
		page.HasMore = true
		page.Items = page.Items[:f.Limit]
	}
	if page.HasMore {
		c.After = page.Items[len(page.Items)-1].Seq
		page.NextCursor = encodeCursor(c)
	}
	err = s.index.QueryRowContext(ctx, "SELECT COUNT(*) FROM activity_projection_events WHERE "+countWhere, countArgs...).Scan(&page.Total)
	return page, err
}
func (s *Service) GetEvent(ctx context.Context, id string) (Event, error) {
	e, err := activitylog.Scan(s.db.QueryRowContext(ctx, "SELECT "+activitylog.Columns+" FROM activity_events WHERE event_id=?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return e, panelerr.NotFound("activity event")
	}
	return e, err
}
func (s *Service) Context(ctx context.Context, id, scope string, before, after int) (Page[Event], error) {
	e, err := s.GetEvent(ctx, id)
	if err != nil {
		return Page[Event]{}, err
	}
	mark, err := s.CatchUp(ctx)
	if err != nil {
		return Page[Event]{}, err
	}
	if e.Seq > mark {
		return Page[Event]{}, panelerr.Conflict("activity_index_pending", "The requested event is saved and its context is being indexed")
	}
	if before < 0 || after < 0 || before+after > 500 {
		return Page[Event]{}, panelerr.BadRequest("activity_context_invalid", "Context must contain at most 500 events")
	}
	where := "1=1"
	args := []any{}
	switch scope {
	case "execution":
		where = "execution_id=?"
		args = append(args, e.ExecutionID)
	case "resource":
		if len(e.Resources) > 0 {
			where = "EXISTS(SELECT 1 FROM json_each(resources_json) r WHERE json_extract(r.value,'$.resourceId')=? AND json_extract(r.value,'$.resourceType')=?)"
			args = append(args, e.Resources[0].ID, e.Resources[0].Type)
		}
	default:
		if e.OperationID != "" {
			where = "operation_id=?"
			args = append(args, e.OperationID)
		} else {
			where = "json_extract(body_json,'$.sourceId')=?"
			args = append(args, e.SourceID)
		}
	}
	out := Page[Event]{Items: []Event{}, IndexState: "ready"}
	head, _ := s.Head(ctx)
	out.HeadSeq = head
	out.SnapshotSeq = mark
	out.ProjectedThroughSeq = mark
	if mark < head {
		out.IndexState = "catching_up"
	}
	for _, part := range []struct {
		op, order string
		n         int
	}{{"<=", "DESC", before + 1}, {">", "ASC", after}} {
		a := append(append([]any{}, args...), e.Seq, mark, part.n)
		rows, err := s.index.QueryContext(ctx, "SELECT body_json FROM activity_projection_events WHERE "+where+" AND seq"+part.op+"? AND seq<=? ORDER BY seq "+part.order+" LIMIT ?", a...)
		if err != nil {
			return out, err
		}
		for rows.Next() {
			var b []byte
			var item Event
			if err = rows.Scan(&b); err != nil {
				rows.Close()
				return out, err
			}
			if err = json.Unmarshal(b, &item); err != nil {
				rows.Close()
				return out, err
			}
			out.Items = append(out.Items, item)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return out, err
		}
	}
	sort.Slice(out.Items, func(i, j int) bool { return out.Items[i].Seq < out.Items[j].Seq })
	out.Total = len(out.Items)
	return out, nil
}
func (s *Service) Operations(ctx context.Context, f Filter) (Page[Operation], error) {
	f.Limit = limit(f.Limit)
	c, head, mark, err := s.bounds(ctx, f)
	out := Page[Operation]{Items: []Operation{}, SnapshotSeq: c.Snapshot, HeadSeq: head, ProjectedThroughSeq: mark, IndexState: "ready"}
	if mark < head {
		out.IndexState = "catching_up"
	}
	if err != nil {
		return out, err
	}
	where, args := filterSQL(f)
	query := `SELECT v.body_json FROM activity_operation_versions v JOIN (SELECT operation_id,MAX(applied_seq) AS n FROM activity_operation_versions WHERE applied_seq<=? GROUP BY operation_id) latest ON latest.operation_id=v.operation_id AND latest.n=v.applied_seq WHERE v.operation_id IN (SELECT operation_id FROM activity_projection_events WHERE seq<=? AND ` + where + `)`
	a := []any{c.Snapshot, c.Snapshot}
	a = append(a, args...)
	for _, x := range []struct{ key, val string }{{"phase", f.Phase}, {"result", f.Result}, {"attention", f.Attention}, {"hadError", f.HadError}} {
		if x.val != "" {
			query += " AND json_extract(v.body_json,'$." + x.key + "')=?"
			if x.val == "true" {
				a = append(a, 1)
			} else if x.val == "false" {
				a = append(a, 0)
			} else {
				a = append(a, x.val)
			}
		}
	}
	countQuery := "SELECT COUNT(*) FROM (" + query + ")"
	countArgs := append([]any{}, a...)
	if c.After > 0 {
		query += " AND json_extract(v.body_json,'$.firstSeq')<?"
		a = append(a, c.After)
	}
	query += " ORDER BY json_extract(v.body_json,'$.firstSeq') DESC LIMIT ?"
	a = append(a, f.Limit+1)
	rows, err := s.index.QueryContext(ctx, query, a...)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var b []byte
		var op Operation
		if err = rows.Scan(&b); err != nil {
			rows.Close()
			return out, err
		}
		if err = json.Unmarshal(b, &op); err != nil {
			rows.Close()
			return out, err
		}
		out.Items = append(out.Items, op)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	if len(out.Items) > f.Limit {
		out.HasMore = true
		out.Items = out.Items[:f.Limit]
		c.After = out.Items[len(out.Items)-1].FirstSeq
		out.NextCursor = encodeCursor(c)
	}
	err = s.index.QueryRowContext(ctx, countQuery, countArgs...).Scan(&out.Total)
	if err == nil {
		err = s.refreshOperationMetrics(ctx, out.Items, c.Snapshot)
	}
	return out, err
}
func (s *Service) Operation(ctx context.Context, id string, snapshot, minSeq int64) (Detail, error) {
	f := Filter{OperationID: id, SnapshotSeq: snapshot, Limit: 100}
	c, head, mark, err := s.bounds(ctx, f)
	out := Detail{Events: []Event{}, Executions: []Execution{}, Steps: []Step{}, RelatedOperations: []string{}, AvailableCommands: []Command{}, SnapshotSeq: c.Snapshot, HeadSeq: head}
	if err != nil {
		return out, err
	}
	if minSeq > mark {
		return out, panelerr.Conflict("activity_index_pending", "The records have been saved and are being indexed")
	}
	var raw []byte
	err = s.index.QueryRowContext(ctx, `SELECT state_json FROM activity_operation_versions WHERE operation_id=? AND applied_seq<=? ORDER BY applied_seq DESC LIMIT 1`, id, c.Snapshot).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return out, panelerr.NotFound("operation")
	}
	if err != nil {
		return out, err
	}
	var state operationState
	if err = json.Unmarshal(raw, &state); err != nil {
		return out, err
	}
	out.Operation = state.Operation
	for _, ex := range state.Executions {
		out.Executions = append(out.Executions, ex)
	}
	sort.Slice(out.Executions, func(i, j int) bool { return out.Executions[i].ExecutionID < out.Executions[j].ExecutionID })
	for _, st := range state.Steps {
		out.Steps = append(out.Steps, st)
	}
	sort.Slice(out.Steps, func(i, j int) bool { return out.Steps[i].StepID < out.Steps[j].StepID })
	if s.Commands != nil {
		out.AvailableCommands = s.Commands(ctx, out.Executions)
	}
	out.RelatedOperations = state.Related
	f.SnapshotSeq = c.Snapshot
	p, err := s.Events(ctx, f)
	if err != nil {
		return out, err
	}
	out.Operation.EventCount = p.Total
	if len(p.Items) > 0 {
		out.Operation.LastSeq = p.Items[0].Seq
		out.Operation.UpdatedAt = p.Items[0].RecordedAt
	}
	out.Events = p.Items
	out.HasMore = p.HasMore
	out.NextCursor = p.NextCursor
	return out, nil
}
func (s *Service) Summary(ctx context.Context, f Filter) (map[string]any, error) {
	c, head, mark, err := s.bounds(ctx, f)
	if err != nil {
		return nil, err
	}
	where, args := filterSQL(f)
	where += " AND seq<=?"
	args = append(args, c.Snapshot)
	result := map[string]any{"total": 0, "snapshotSeq": c.Snapshot, "headSeq": head, "projectedThroughSeq": mark}
	total := 0
	for _, pair := range []struct{ column, key string }{{"level", "byLevel"}, {"domain", "byDomain"}} {
		counts := map[string]int{}
		rows, err := s.index.QueryContext(ctx, "SELECT "+pair.column+",COUNT(*) FROM activity_projection_events WHERE "+where+" GROUP BY "+pair.column, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var k string
			var n int
			if err = rows.Scan(&k, &n); err != nil {
				rows.Close()
				return nil, err
			}
			counts[k] = n
			if pair.key == "byLevel" {
				total += n
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		result[pair.key] = counts
	}
	result["total"] = total
	capacity, err := activitylog.Capacity(ctx, s.db)
	if err != nil {
		return nil, err
	}
	result["capacity"] = capacity
	return result, nil
}
func parseInt(s string, def int) (int, error) {
	if s == "" {
		return def, nil
	}
	n, e := strconv.Atoi(s)
	if e != nil {
		return 0, fmt.Errorf("invalid integer")
	}
	return n, nil
}

func (s *Service) refreshOperationMetrics(ctx context.Context, operations []Operation, snapshot int64) error {
	if len(operations) == 0 {
		return nil
	}
	args := []any{snapshot}
	positions := map[string]int{}
	for i, op := range operations {
		args = append(args, op.OperationID)
		positions[op.OperationID] = i
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(operations)), ",")
	rows, err := s.index.QueryContext(ctx, `SELECT m.operation_id,m.n,e.seq,e.recorded_at FROM (SELECT operation_id,COUNT(*) n,MAX(seq) last FROM activity_projection_events WHERE seq<=? AND operation_id IN (`+marks+`) GROUP BY operation_id) m JOIN activity_projection_events e ON e.seq=m.last`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, at string
		var n int
		var seq int64
		if err = rows.Scan(&id, &n, &seq, &at); err != nil {
			return err
		}
		i := positions[id]
		operations[i].EventCount = n
		operations[i].LastSeq = seq
		operations[i].UpdatedAt, err = time.Parse(time.RFC3339Nano, at)
		if err != nil {
			return err
		}
	}
	return rows.Err()
}

// OperationReceipt resolves a response's explicit operation identity to a real
// durable fact. It never invents a record for an uncommitted operation.
func (s *Service) OperationReceipt(ctx context.Context, operationID string) (Event, error) {
	return activitylog.Scan(s.db.QueryRowContext(ctx, `SELECT `+activitylog.Columns+` FROM activity_events WHERE operation_id=? ORDER BY seq DESC LIMIT 1`, operationID))
}
