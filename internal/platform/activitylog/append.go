package activitylog

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	panelerr "panel/internal/platform/errors"
	id "panel/internal/platform/identity"
)

type Scanner interface{ Scan(...any) error }

func Scan(row Scanner) (Event, error) {
	var e Event
	var occurred, recorded string
	var initiator, resources, args, data []byte
	err := row.Scan(&e.Seq, &e.EventID, &e.EventVersion, &e.EventType, &e.Kind, &e.Level, &e.Domain, &e.Action, &e.OperationID, &e.RunID, &e.ExecutionID, &e.StepID, &e.ParentStepID, &e.CausationEventID, &e.SourceID, &e.SourceEpoch, &e.SourceStreamID, &e.SourceSeq, &occurred, &recorded, &e.Actor.Kind, &e.Actor.ID, &e.Actor.Name, &initiator, &resources, &e.Trigger, &e.RequestID, &e.Stream, &e.MessageCode, &args, &e.Text, &data, &e.ContentHash)
	if err != nil {
		return e, err
	}
	if e.OccurredAt, err = parseTime(occurred); err != nil {
		return e, err
	}
	if e.RecordedAt, err = parseTime(recorded); err != nil {
		return e, err
	}
	for _, v := range []struct {
		b  []byte
		to any
	}{{initiator, &e.Initiator}, {resources, &e.Resources}, {args, &e.MessageArgs}, {data, &e.Data}} {
		if err = json.Unmarshal(v.b, v.to); err != nil {
			return e, err
		}
	}
	return e, nil
}
func parseTime(s string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05"} {
		if t, e := time.Parse(layout, s); e == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid activity timestamp %q", s)
}
func Append(ctx context.Context, db *sql.DB, inputs []EventInput) (Receipt, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Receipt{}, err
	}
	defer tx.Rollback()
	receipt, err := AppendTx(ctx, tx, inputs)
	if err != nil {
		return Receipt{}, err
	}
	if err = tx.Commit(); err != nil {
		return Receipt{}, err
	}
	return receipt, nil
}
func AppendTx(ctx context.Context, tx *sql.Tx, inputs []EventInput) (Receipt, error) {
	receipt := Receipt{Events: []EventReceipt{}}
	if len(inputs) > 500 {
		return receipt, panelerr.BadRequest("activity_batch_too_large", "At most 500 events can be appended in one transaction")
	}
	for _, in := range inputs {
		if in.EventType == "operation.requested" || in.EventType == "run.requested" {
			if err := CheckAdmission(ctx, tx); err != nil {
				return Receipt{}, err
			}
		}
		if in.CausationEventID == "" {
			in.CausationEventID = CauseFromContext(ctx)
		}
		if in.EventType == "" {
			return Receipt{}, panelerr.Validation("activity_event_type_required", "Event type is required")
		}
		if in.EventID == "" {
			in.EventID = id.New("evt")
		}
		if in.EventVersion == 0 {
			in.EventVersion = 1
		}
		if in.Kind == "" {
			in.Kind = "lifecycle"
		}
		if in.Level == "" {
			in.Level = "info"
		}
		if in.Domain == "" {
			in.Domain = "system"
		}
		if in.SourceID == "" {
			in.SourceID = "panel"
		}
		if in.SourceEpoch == "" {
			in.SourceEpoch = "panel"
		}
		if in.SourceStreamID == "" {
			in.SourceStreamID = "system"
		}
		if in.Actor.Kind == "" {
			in.Actor = ActorFromContext(ctx)
			if in.Actor.Kind == "" {
				in.Actor.Kind = "system"
			}
		}
		if in.Initiator.Kind == "" {
			in.Initiator = in.Actor
		}
		if in.Resources == nil {
			in.Resources = []Resource{}
		}
		if in.Data == nil {
			in.Data = map[string]any{}
		}
		if in.MessageArgs == nil {
			in.MessageArgs = map[string]any{}
		}
		in.Text = Redact(in.Text)
		in.Data = redactValue(in.Data).(map[string]any)
		in.MessageArgs = redactValue(in.MessageArgs).(map[string]any)
		for i := range in.Resources {
			if in.Resources[i].Snapshot != nil {
				in.Resources[i].Snapshot = redactValue(in.Resources[i].Snapshot).(map[string]any)
			}
		}
		now := time.Now().UTC()
		existing, lookupErr := Scan(tx.QueryRowContext(ctx, "SELECT "+Columns+" FROM activity_events WHERE event_id=?", in.EventID))
		if lookupErr != nil && !errors.Is(lookupErr, sql.ErrNoRows) {
			return Receipt{}, lookupErr
		}
		if in.OccurredAt.IsZero() {
			if lookupErr == nil {
				in.OccurredAt = existing.OccurredAt
			} else {
				in.OccurredAt = now
			}
		}
		if in.SourceSeq == 0 {
			if lookupErr == nil {
				in.SourceSeq = existing.SourceSeq
			} else if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(source_seq),0)+1 FROM activity_events WHERE source_id=? AND source_epoch=? AND source_stream_id=?`, in.SourceID, in.SourceEpoch, in.SourceStreamID).Scan(&in.SourceSeq); err != nil {
				return Receipt{}, err
			}
		}
		payload, err := json.Marshal(in)
		if err != nil {
			return Receipt{}, err
		}
		digest := sha256.Sum256(payload)
		hash := hex.EncodeToString(digest[:])
		if lookupErr == nil {
			if existing.ContentHash != hash {
				return Receipt{}, panelerr.Conflict("activity_event_id_conflict", "An event identity cannot be reused with different content")
			}
			receipt.Events = append(receipt.Events, EventReceipt{existing.EventID, existing.Seq})
			continue
		}
		var found string
		err = tx.QueryRowContext(ctx, `SELECT event_id FROM activity_events WHERE source_id=? AND source_epoch=? AND source_stream_id=? AND source_seq=?`, in.SourceID, in.SourceEpoch, in.SourceStreamID, in.SourceSeq).Scan(&found)
		if err == nil {
			return Receipt{}, panelerr.Conflict("activity_source_sequence_conflict", "The source sequence already belongs to another event")
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return Receipt{}, err
		}
		fields := []any{in.EventID, in.EventVersion, in.EventType, in.Kind, in.Level, in.Domain, in.Action, in.OperationID, in.RunID, in.ExecutionID, in.StepID, in.ParentStepID, in.CausationEventID, in.SourceID, in.SourceEpoch, in.SourceStreamID, in.SourceSeq, in.OccurredAt.UTC().Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), in.Actor.Kind, in.Actor.ID, in.Actor.Name, jsonString(in.Initiator), jsonString(in.Resources), in.Trigger, in.RequestID, in.Stream, in.MessageCode, jsonString(in.MessageArgs), in.Text, jsonString(in.Data), hash}
		result, err := tx.ExecContext(ctx, `INSERT INTO activity_events(`+strings.TrimPrefix(Columns, "seq,")+`) VALUES(`+strings.TrimSuffix(strings.Repeat("?,", len(fields)), ",")+`)`, fields...)
		if err != nil {
			return Receipt{}, err
		}
		seq, err := result.LastInsertId()
		if err != nil {
			return Receipt{}, err
		}
		receipt.Events = append(receipt.Events, EventReceipt{in.EventID, seq})
		if in.OperationID != "" && (in.EventType == "operation.requested" || in.EventType == "retry.requested" || in.EventType == "run.requested") {
			RecordReceipt(ctx, in.OperationID, in.EventID, seq)
		}
	}
	err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0) FROM activity_events`).Scan(&receipt.HeadSeq)
	return receipt, err
}
func jsonString(v any) string { b, _ := json.Marshal(v); return string(b) }
