// Package notifications is the in-memory event notification channel behind
// GET /api/v1/notifications (ENG-EVT-011).
//
// It is deliberately independent from the database: process events are
// published here from logging.RecordContext before the events row is upserted
// (and even when no events store is configured), so the notification channel
// keeps working when AppDB is slow or absent. The channel only carries process
// event lifecycle transitions (start + terminal), never HTTP request logs.
package notifications

import (
	"context"
	"strings"
	"sync"
	"time"
)

// BufferSize is the ring buffer capacity. Older notifications are dropped.
const BufferSize = 256

// MaxTrackedEvents bounds the eventId dedup table so a long-running Panel does
// not accumulate one entry per process event forever.
const MaxTrackedEvents = 1024

// Notification is one product notification item for GET /api/v1/notifications.
type Notification struct {
	Seq       uint64 `json:"seq"`
	EventID   string `json:"eventId"`
	Module    string `json:"module"`
	Title     string `json:"title"`
	Summary   string `json:"summary,omitempty"`
	State     string `json:"state,omitempty"`
	Level     string `json:"level"`
	Origin    string `json:"origin,omitempty"`
	HadError  bool   `json:"hadError"`
	UpdatedAt string `json:"updatedAt"` // RFC3339Nano UTC
}

// ProcessEvent is a publishable process event transition. It mirrors
// Notification without the hub-assigned Seq.
type ProcessEvent struct {
	EventID   string `json:"eventId"`
	Module    string `json:"module"`
	Title     string `json:"title"`
	Summary   string `json:"summary,omitempty"`
	State     string `json:"state,omitempty"`
	Level     string `json:"level"`
	Origin    string `json:"origin,omitempty"`
	HadError  bool   `json:"hadError"`
	UpdatedAt string `json:"updatedAt,omitempty"` // RFC3339Nano UTC; now when empty
}

// Default is the process-wide notification hub.
var Default = NewHub()

// PublishProcess publishes one process event transition on Default.
func PublishProcess(ev ProcessEvent) { Default.PublishProcess(ev) }

// Wait waits for notifications newer than after (see Hub.Wait).
func Wait(ctx context.Context, after uint64, timeout time.Duration) ([]Notification, uint64) {
	return Default.Wait(ctx, after, timeout)
}

// ResetForTest clears Default in place. It exists for tests only.
func ResetForTest() { Default.reset() }

type trackState struct {
	started  bool
	terminal bool
}

// Hub is a concurrency-safe in-memory ring buffer of process notifications with
// monotonic sequence numbers and per-event lifecycle dedup.
type Hub struct {
	mu    sync.Mutex
	seq   uint64
	buf   [BufferSize]Notification
	head  int // index of the oldest buffered notification
	size  int // buffered notification count
	track map[string]*trackState
	order []string // eventIds in first-seen order for bounded tracking

	changed chan struct{}
}

// NewHub returns an empty hub. Sequence numbers start at 1.
func NewHub() *Hub {
	return &Hub{
		track:   make(map[string]*trackState),
		changed: make(chan struct{}),
	}
}

// PublishProcess records one process event transition.
//
// eventId-less and module=="http" events are ignored. Per eventId at most one
// "start" notification (the first non-terminal transition) and one terminal
// notification (succeeded/success/failed/failure/cancelled/superseded/partial/
// partial_success) are published; repeating or trailing updates are skipped.
// Publishing never blocks on readers.
func (h *Hub) PublishProcess(ev ProcessEvent) {
	eventID := strings.TrimSpace(ev.EventID)
	module := strings.TrimSpace(ev.Module)
	if eventID == "" || module == "http" {
		return
	}
	state := strings.TrimSpace(ev.State)
	terminal := isTerminalState(state)

	// Input validation above is cheap; hold the lock only for state mutation.
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.changed == nil {
		h.changed = make(chan struct{})
	}
	tr := h.trackLocked(eventID)
	if terminal {
		if tr.terminal {
			return
		}
		tr.terminal = true
	} else {
		if tr.started || tr.terminal {
			return
		}
		tr.started = true
	}

	updatedAt := strings.TrimSpace(ev.UpdatedAt)
	if updatedAt == "" {
		updatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	h.seq++
	h.appendLocked(Notification{
		Seq:       h.seq,
		EventID:   eventID,
		Module:    module,
		Title:     ev.Title,
		Summary:   ev.Summary,
		State:     state,
		Level:     ev.Level,
		Origin:    ev.Origin,
		HadError:  ev.HadError,
		UpdatedAt: updatedAt,
	})
	// Broadcast: closing wakes every waiter; each waiter re-checks the buffer.
	close(h.changed)
	h.changed = make(chan struct{})
}

// Wait returns notifications with Seq > after.
//
//   - after == 0 or after > latest: immediate baseline, (nil, latest); history
//     is never replayed, so a fresh client cannot flood the toast channel.
//   - buffered notifications newer than after: returned immediately, cursor is
//     the last item's Seq.
//   - otherwise it blocks up to timeout, returning as soon as a notification is
//     published. timeout <= 0 means "check once, do not block". A timeout or
//     cancelled ctx returns (nil, after).
func (h *Hub) Wait(ctx context.Context, after uint64, timeout time.Duration) ([]Notification, uint64) {
	if ctx == nil {
		ctx = context.Background()
	}
	var timeoutC <-chan time.Time
	if timeout > 0 {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		timeoutC = timer.C
	}
	for {
		h.mu.Lock()
		latest := h.seq
		if after == 0 || after > latest {
			h.mu.Unlock()
			return nil, latest
		}
		if items := h.itemsAfterLocked(after); len(items) > 0 {
			h.mu.Unlock()
			return items, items[len(items)-1].Seq
		}
		changed := h.changed
		h.mu.Unlock()

		if timeoutC == nil {
			return nil, after
		}
		select {
		case <-changed:
			// Re-check the buffer: other waiters may have consumed nothing
			// (items are never consumed), so this only loops for new publishes.
		case <-ctx.Done():
			return nil, after
		case <-timeoutC:
			return nil, after
		}
	}
}

func (h *Hub) appendLocked(n Notification) {
	if h.size < BufferSize {
		h.buf[(h.head+h.size)%BufferSize] = n
		h.size++
		return
	}
	// Full: overwrite the oldest entry and advance the head.
	h.buf[h.head] = n
	h.head = (h.head + 1) % BufferSize
}

func (h *Hub) itemsAfterLocked(after uint64) []Notification {
	items := make([]Notification, 0, h.size)
	for i := 0; i < h.size; i++ {
		n := h.buf[(h.head+i)%BufferSize]
		if n.Seq > after {
			items = append(items, n)
		}
	}
	return items
}

// trackLocked returns the dedup state for eventID, evicting the oldest entry
// when the tracking table is full.
func (h *Hub) trackLocked(eventID string) *trackState {
	if h.track == nil {
		h.track = make(map[string]*trackState)
	}
	if tr, ok := h.track[eventID]; ok {
		return tr
	}
	if len(h.order) >= MaxTrackedEvents {
		oldest := h.order[0]
		h.order = h.order[1:]
		delete(h.track, oldest)
	}
	tr := &trackState{}
	h.track[eventID] = tr
	h.order = append(h.order, eventID)
	return tr
}

func (h *Hub) reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seq = 0
	h.head = 0
	h.size = 0
	h.track = make(map[string]*trackState)
	h.order = h.order[:0]
	if h.changed != nil {
		close(h.changed)
	}
	h.changed = make(chan struct{})
}

// isTerminalState reports whether state closes an event lifecycle. The list is
// frozen by the notification contract; anything else publishes as a start.
func isTerminalState(state string) bool {
	switch strings.ToLower(state) {
	case "succeeded", "success", "failed", "failure", "cancelled", "superseded", "partial", "partial_success":
		return true
	default:
		return false
	}
}
