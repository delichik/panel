package events

import (
	"context"
	"sync"
	"time"

	"panel/internal/platform/logging"
)

const (
	asyncQueueCapacity    = 2048
	asyncBatchSize        = 64
	asyncFlushInterval    = 50 * time.Millisecond
	asyncKnownPreseedCap  = 4096
	asyncKnownMaxCapacity = 8192
)

type asyncOpKind uint8

const (
	asyncOpUpsert asyncOpKind = iota + 1
	asyncOpTouch
	asyncOpFlush
)

type asyncOp struct {
	kind      asyncOpKind
	record    *Record
	touch     touchOp
	flushDone chan struct{}
}

// AsyncStore wraps Store with a bounded coalescing queue and a known-ID filter
// so ordinary log lines never acquire SQLite write locks for non-process logs
// and process metadata writes are micro-batched off request/tx paths.
type AsyncStore struct {
	store     *Store
	ch        chan asyncOp
	stopCh    chan struct{}
	done      chan struct{}
	closeOnce sync.Once

	knownMu  sync.RWMutex
	knownIDs map[string]struct{}
}

var _ logging.ContextStore = (*AsyncStore)(nil)

// NewAsyncStore starts a background micro-batcher over store.
func NewAsyncStore(store *Store) *AsyncStore {
	a := &AsyncStore{
		store:    store,
		ch:       make(chan asyncOp, asyncQueueCapacity),
		stopCh:   make(chan struct{}),
		done:     make(chan struct{}),
		knownIDs: make(map[string]struct{}, 512),
	}
	a.preseedKnownIDs()
	go a.run()
	return a
}

func (a *AsyncStore) preseedKnownIDs() {
	if a.store == nil || a.store.db == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	rows, err := a.store.db.QueryContext(ctx, `SELECT id FROM events ORDER BY updated_at DESC LIMIT ?`, asyncKnownPreseedCap)
	if err != nil {
		return
	}
	defer rows.Close()
	a.knownMu.Lock()
	defer a.knownMu.Unlock()
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil && id != "" {
			a.knownIDs[id] = struct{}{}
		}
	}
}

func (a *AsyncStore) isKnown(eventID string) bool {
	a.knownMu.RLock()
	_, ok := a.knownIDs[eventID]
	a.knownMu.RUnlock()
	return ok
}

func (a *AsyncStore) markKnown(eventID string) {
	a.knownMu.Lock()
	if len(a.knownIDs) >= asyncKnownMaxCapacity {
		clear(a.knownIDs)
	}
	a.knownIDs[eventID] = struct{}{}
	a.knownMu.Unlock()
}

// ResetKnownIDs clears the in-memory known-event cache (called when events are cleared).
func (a *AsyncStore) ResetKnownIDs() {
	if a == nil {
		return
	}
	a.knownMu.Lock()
	clear(a.knownIDs)
	a.knownMu.Unlock()
}

// Upsert registers the event ID as known and enqueues a micro-batched upsert.
func (a *AsyncStore) Upsert(_ context.Context, row logging.ContextRecord) error {
	if a == nil || a.store == nil || a.store.db == nil {
		return nil
	}
	rec, ok := prepareUpsertRecord(row)
	if !ok {
		return nil
	}
	a.markKnown(rec.ID)
	select {
	case <-a.stopCh:
		return nil
	default:
	}
	select {
	case a.ch <- asyncOp{kind: asyncOpUpsert, record: &rec}:
	case <-a.stopCh:
	default:
		// Bounded queue saturated: drop diagnostic index write without blocking caller (ARCH-CON-005).
	}
	return nil
}

// Touch enqueues an update only for event IDs already registered via Upsert.
func (a *AsyncStore) Touch(_ context.Context, eventID string, level string, msg string) error {
	if a == nil || a.store == nil || a.store.db == nil {
		return nil
	}
	op, ok := prepareTouch(eventID, level, msg)
	if !ok || !a.isKnown(op.eventID) {
		return nil
	}
	select {
	case <-a.stopCh:
		return nil
	default:
	}
	select {
	case a.ch <- asyncOp{kind: asyncOpTouch, touch: op}:
	case <-a.stopCh:
	default:
	}
	return nil
}

// Flush waits until all ops enqueued before this call have been committed.
func (a *AsyncStore) Flush(ctx context.Context) error {
	if a == nil {
		return nil
	}
	done := make(chan struct{})
	select {
	case <-a.stopCh:
		<-a.done
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case a.ch <- asyncOp{kind: asyncOpFlush, flushDone: done}:
	}
	select {
	case <-done:
		return nil
	case <-a.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close stops accepting new ops, drains buffered ops, and waits for exit.
func (a *AsyncStore) Close() error {
	if a == nil {
		return nil
	}
	a.closeOnce.Do(func() {
		close(a.stopCh)
	})
	<-a.done
	return nil
}

func (a *AsyncStore) run() {
	defer close(a.done)
	batch := make([]asyncOp, 0, asyncBatchSize)
	timer := time.NewTimer(asyncFlushInterval)
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	defer timer.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		a.flushBatch(batch)
		batch = batch[:0]
	}

	for {
		var first asyncOp
		select {
		case <-a.stopCh:
			a.drainRemaining(batch)
			return
		case first = <-a.ch:
		}

		batch = append(batch, first)
		if first.kind == asyncOpFlush {
			flush()
			continue
		}

		timer.Reset(asyncFlushInterval)
		collecting := true
		for collecting && len(batch) < asyncBatchSize {
			select {
			case <-a.stopCh:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				a.drainRemaining(batch)
				return
			case op := <-a.ch:
				batch = append(batch, op)
				if op.kind == asyncOpFlush {
					collecting = false
				}
			case <-timer.C:
				collecting = false
			}
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		flush()
	}
}

func (a *AsyncStore) drainRemaining(batch []asyncOp) {
	for {
		select {
		case op := <-a.ch:
			batch = append(batch, op)
			if len(batch) >= asyncBatchSize {
				a.flushBatch(batch)
				batch = batch[:0]
			}
		default:
			if len(batch) > 0 {
				a.flushBatch(batch)
			}
			return
		}
	}
}

func (a *AsyncStore) flushBatch(ops []asyncOp) {
	var flushChans []chan struct{}
	defer func() {
		for _, ch := range flushChans {
			close(ch)
		}
	}()

	type coalescedKind uint8
	const (
		cUpsert coalescedKind = iota + 1
		cTouch
	)
	type coalescedOp struct {
		kind   coalescedKind
		record *Record
		touch  touchOp
	}

	coalesced := make([]coalescedOp, 0, len(ops))
	touchIndex := make(map[string]int, len(ops))

	for _, op := range ops {
		switch op.kind {
		case asyncOpFlush:
			if op.flushDone != nil {
				flushChans = append(flushChans, op.flushDone)
			}
		case asyncOpUpsert:
			if op.record == nil {
				continue
			}
			delete(touchIndex, op.record.ID)
			coalesced = append(coalesced, coalescedOp{kind: cUpsert, record: op.record})
		case asyncOpTouch:
			if idx, ok := touchIndex[op.touch.eventID]; ok {
				existing := &coalesced[idx].touch
				existing.updatedAt = op.touch.updatedAt
				if op.touch.level != "" {
					existing.level = op.touch.level
				}
				if op.touch.hadError != 0 {
					existing.hadError = 1
				}
				if op.touch.summary != "" {
					existing.summary = op.touch.summary
				}
				continue
			}
			touchIndex[op.touch.eventID] = len(coalesced)
			coalesced = append(coalesced, coalescedOp{kind: cTouch, touch: op.touch})
		}
	}

	if len(coalesced) == 0 || a.store == nil || a.store.db == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := a.store.db.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	for _, op := range coalesced {
		switch op.kind {
		case cUpsert:
			_ = execUpsert(ctx, tx, *op.record)
		case cTouch:
			_ = execTouch(ctx, tx, op.touch)
		}
	}
	if tx.Commit() == nil {
		committed = true
	}
}
