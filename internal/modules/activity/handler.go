package activity

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	panelerr "panel/internal/platform/errors"
	httpx "panel/internal/platform/http"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service} }
func queryFilter(r *http.Request) (Filter, error) {
	q := r.URL.Query()
	allowed := map[string]bool{}
	for _, k := range []string{"q", "domain", "action", "kind", "level", "trigger", "actorId", "resourceType", "resourceId", "operationId", "executionId", "stepId", "phase", "result", "attention", "hadError", "from", "to", "cursor", "limit", "snapshotSeq", "afterSeq", "minSeq", "scope", "before", "after"} {
		allowed[k] = true
	}
	for k, values := range q {
		if !allowed[k] || len(values) != 1 {
			return Filter{}, panelerr.BadRequest("activity_query_invalid", "Unknown or repeated activity query parameter: "+k)
		}
	}
	f := Filter{Q: q.Get("q"), Domain: q.Get("domain"), Action: q.Get("action"), Kind: q.Get("kind"), Level: q.Get("level"), Trigger: q.Get("trigger"), ActorID: q.Get("actorId"), ResourceType: q.Get("resourceType"), ResourceID: q.Get("resourceId"), OperationID: q.Get("operationId"), ExecutionID: q.Get("executionId"), StepID: q.Get("stepId"), Phase: q.Get("phase"), Result: q.Get("result"), Attention: q.Get("attention"), HadError: q.Get("hadError"), Cursor: q.Get("cursor")}
	var err error
	f.Limit, err = parseInt(q.Get("limit"), 100)
	if err != nil || f.Limit < 1 || f.Limit > 500 {
		return f, panelerr.BadRequest("activity_limit_invalid", "limit must be between 1 and 500")
	}
	for _, v := range []struct {
		k  string
		to *int64
	}{{"snapshotSeq", &f.SnapshotSeq}, {"afterSeq", &f.AfterSeq}} {
		if q.Get(v.k) != "" {
			*v.to, err = strconv.ParseInt(q.Get(v.k), 10, 64)
			if err != nil || *v.to < 0 {
				return f, panelerr.BadRequest("activity_sequence_invalid", "Sequence must be a non-negative integer")
			}
		}
	}
	for _, v := range []struct {
		k  string
		to **time.Time
	}{{"from", &f.From}, {"to", &f.To}} {
		if q.Get(v.k) != "" {
			t, e := time.Parse(time.RFC3339Nano, q.Get(v.k))
			if e != nil {
				return f, panelerr.BadRequest("activity_time_invalid", "Time must use RFC3339")
			}
			*v.to = &t
		}
	}
	if f.From != nil && f.To != nil && f.From.After(*f.To) {
		return f, panelerr.BadRequest("activity_time_range_invalid", "from must not be after to")
	}
	for _, v := range []string{f.Attention, f.HadError} {
		if v != "" && v != "true" && v != "false" {
			return f, panelerr.BadRequest("activity_boolean_invalid", "Boolean filters must be true or false")
		}
	}
	return f, nil
}
func respond(w http.ResponseWriter, v any, err error) {
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, v)
}
func (h *Handler) Events(w http.ResponseWriter, r *http.Request) {
	f, e := queryFilter(r)
	if e != nil {
		respond(w, nil, e)
		return
	}
	v, e := h.service.Events(r.Context(), f)
	respond(w, v, e)
}
func (h *Handler) Tail(w http.ResponseWriter, r *http.Request) {
	f, e := queryFilter(r)
	if e != nil {
		respond(w, nil, e)
		return
	}
	v, e := h.service.Tail(r.Context(), f)
	respond(w, v, e)
}
func (h *Handler) Operations(w http.ResponseWriter, r *http.Request) {
	f, e := queryFilter(r)
	if e != nil {
		respond(w, nil, e)
		return
	}
	v, e := h.service.Operations(r.Context(), f)
	respond(w, v, e)
}
func (h *Handler) Event(w http.ResponseWriter, r *http.Request) {
	v, e := h.service.GetEvent(r.Context(), r.PathValue("id"))
	respond(w, v, e)
}
func (h *Handler) Operation(w http.ResponseWriter, r *http.Request) {
	f, e := queryFilter(r)
	if e != nil {
		respond(w, nil, e)
		return
	}
	min, e := parseInt(r.URL.Query().Get("minSeq"), 0)
	if e != nil || min < 0 {
		respond(w, nil, panelerr.BadRequest("activity_sequence_invalid", "Invalid minSeq"))
		return
	}
	v, e := h.service.Operation(r.Context(), r.PathValue("id"), f.SnapshotSeq, int64(min))
	respond(w, v, e)
}
func (h *Handler) Context(w http.ResponseWriter, r *http.Request) {
	_, e := queryFilter(r)
	if e != nil {
		respond(w, nil, e)
		return
	}
	before, e := parseInt(r.URL.Query().Get("before"), 25)
	if e != nil {
		respond(w, nil, panelerr.BadRequest("activity_context_invalid", "Invalid before"))
		return
	}
	after, e := parseInt(r.URL.Query().Get("after"), 25)
	if e != nil {
		respond(w, nil, panelerr.BadRequest("activity_context_invalid", "Invalid after"))
		return
	}
	v, e := h.service.Context(r.Context(), r.PathValue("id"), r.URL.Query().Get("scope"), before, after)
	respond(w, v, e)
}
func (h *Handler) Summary(w http.ResponseWriter, r *http.Request) {
	f, e := queryFilter(r)
	if e != nil {
		respond(w, nil, e)
		return
	}
	v, e := h.service.Summary(r.Context(), f)
	respond(w, v, e)
}
func (h *Handler) Export(w http.ResponseWriter, r *http.Request) {
	f, e := queryFilter(r)
	if e != nil {
		respond(w, nil, e)
		return
	}
	f.Limit = 500
	page, e := h.service.Events(r.Context(), f)
	if e != nil {
		respond(w, nil, e)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="activity.jsonl"`)
	enc := json.NewEncoder(w)
	if enc.Encode(map[string]any{"type": "manifest", "formatVersion": 1, "snapshotSeq": page.SnapshotSeq, "total": page.Total, "filters": r.URL.Query(), "completeness": "See stream.closed and evidence.gap events; transport completeness is not implied by operation completion"}) != nil {
		return
	}
	for {
		for _, event := range page.Items {
			if enc.Encode(event) != nil {
				return
			}
		}
		if !page.HasMore {
			break
		}
		f.Cursor = page.NextCursor
		page, e = h.service.Events(r.Context(), f)
		if e != nil {
			_ = enc.Encode(map[string]any{"type": "export.error", "message": e.Error()})
			return
		}
	}
}
func (h *Handler) Evidence(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var sealed int
	if err := h.service.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM activity_events WHERE event_type='evidence.sealed' AND json_extract(data_json,'$.evidenceId')=?`, id).Scan(&sealed); err != nil {
		respond(w, nil, err)
		return
	}
	if sealed == 0 {
		respond(w, nil, panelerr.Conflict("activity_evidence_pending", "Evidence has not been sealed"))
		return
	}
	rows, err := h.service.db.QueryContext(r.Context(), `SELECT content,codec FROM activity_evidence_chunks WHERE evidence_id=? ORDER BY chunk_seq`, id)
	if err != nil {
		respond(w, nil, err)
		return
	}
	defer rows.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="evidence.bin"`)
	for rows.Next() {
		var b []byte
		var codec string
		if err = rows.Scan(&b, &codec); err != nil {
			return
		}
		if codec != "identity" {
			return
		}
		if _, err = w.Write(b); err != nil {
			return
		}
	}
}
