package cache

import (
	"container/list"
	"sync"
	"time"
)

type entry struct {
	key       string
	value     []byte
	expiresAt time.Time
	size      int64
	elem      *list.Element
}

type shard struct {
	mu         sync.Mutex
	entries    map[string]*entry
	lru        *list.List
	bytes      int64
	maxBytes   int64
	maxEntries int
}

func newShard(maxBytes int64, maxEntries int) *shard {
	return &shard{
		entries:    make(map[string]*entry),
		lru:        list.New(),
		maxBytes:   maxBytes,
		maxEntries: maxEntries,
	}
}

func (s *shard) removeLocked(e *entry) {
	if e == nil {
		return
	}
	delete(s.entries, e.key)
	if e.elem != nil {
		s.lru.Remove(e.elem)
		e.elem = nil
	}
	s.bytes -= e.size
	if s.bytes < 0 {
		s.bytes = 0
	}
}

func expired(e *entry, now time.Time) bool {
	return !e.expiresAt.IsZero() && !now.Before(e.expiresAt)
}
