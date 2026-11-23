package cache

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

// Errors returned by the cache.
var (
	// ErrValueTooLarge reports that a value exceeds MaxValueBytes or the
	// effective per-shard budget.
	ErrValueTooLarge = errors.New("cache: value exceeds the configured size limit")
	// ErrNotInteger reports that Incr was called on a value that is not a
	// base-10 integer.
	ErrNotInteger = errors.New("cache: value is not an integer")
	// ErrInvalidKey reports an empty, oversized or non-UTF-8 key.
	ErrInvalidKey = errors.New("cache: key must be non-empty UTF-8 and at most 512 bytes")
	// ErrInvalidTTL reports a negative TTL or a TTL above MaxTTL.
	ErrInvalidTTL = errors.New("cache: ttl must be 0 (default) or within (0, MaxTTL]")
	// ErrNilLoad reports GetOrLoad without a loader.
	ErrNilLoad = errors.New("cache: load function is nil")
)

const (
	defaultMaxBytes      = int64(32 << 20) // 32 MiB
	defaultMaxEntries    = 20_000
	defaultDefaultTTL    = 2 * time.Second
	defaultMaxTTL        = 60 * time.Second
	defaultMaxValueBytes = 1 << 20 // 1 MiB
	defaultShards        = 16
	defaultSweepInterval = 10 * time.Second
	defaultLoadTimeout   = 10 * time.Second

	maxKeyLength  = 512
	maxShards     = 256
	entryOverhead = int64(64)
)

// Config controls cache capacity and timing. The zero value is usable.
type Config struct {
	// Disabled makes New return a no-op cache. All methods remain safe to call
	// and GetOrLoad delegates to the loader.
	Disabled bool
	// MaxBytes is the approximate total value budget. 0 means 32 MiB.
	MaxBytes int64
	// MaxEntries is the approximate total entry budget. 0 means 20,000.
	MaxEntries int
	// DefaultTTL is used when a caller passes ttl == 0. 0 means 2s.
	DefaultTTL time.Duration
	// MaxTTL is the hard upper bound for any TTL. 0 means 60s.
	MaxTTL time.Duration
	// MaxValueBytes rejects individual values above this size. 0 means 1 MiB.
	MaxValueBytes int
	// Shards is the number of cache shards. 0 means 16. Non-zero values are
	// rounded up to a power of two and capped at 256.
	Shards int
	// SweepInterval is the background expired-entry sweep period. 0 means 10s.
	SweepInterval time.Duration
	// LoadTimeout bounds a single GetOrLoad loader. 0 means 10s.
	LoadTimeout time.Duration
}

// Stats is a point-in-time snapshot. Counters are cumulative since New.
type Stats struct {
	Enabled      bool          `json:"enabled"`
	Entries      int           `json:"entries"`
	Bytes        int64         `json:"bytes"`
	Hits         uint64        `json:"hits"`
	Misses       uint64        `json:"misses"`
	Evictions    uint64        `json:"evictions"`
	Expired      uint64        `json:"expired"`
	Loads        uint64        `json:"loads"`
	LoadErrors   uint64        `json:"loadErrors"`
	LoadDuration time.Duration `json:"loadDuration"`
}

type counters struct {
	hits       atomic.Uint64
	misses     atomic.Uint64
	evictions  atomic.Uint64
	expired    atomic.Uint64
	loads      atomic.Uint64
	loadErrors atomic.Uint64
	loadNanos  atomic.Int64
}

// Cache is a sharded, in-process key/value cache. It is safe for concurrent
// use. Use New to construct one; the zero value is not usable.
type Cache struct {
	enabled bool
	closed  atomic.Bool
	cfg     Config
	shards  []*shard
	mask    uint64
	now     func() time.Time
	epoch   atomic.Uint64
	stats   counters
	flight  *flightGroup

	stopCh    chan struct{}
	doneCh    chan struct{}
	closeOnce sync.Once
}

// New validates cfg, applies defaults and starts the background sweeper.
func New(cfg Config) (*Cache, error) {
	return newWithClock(cfg, time.Now)
}

func newWithClock(cfg Config, now func() time.Time) (*Cache, error) {
	if now == nil {
		now = time.Now
	}
	normalized, err := normalizeConfig(cfg)
	if err != nil {
		return nil, err
	}
	c := &Cache{
		enabled: !normalized.Disabled,
		cfg:     normalized,
		now:     now,
		flight:  newFlightGroup(),
	}
	c.shards = make([]*shard, normalized.Shards)
	shardMaxBytes := normalized.MaxBytes / int64(normalized.Shards)
	if shardMaxBytes < 1 {
		shardMaxBytes = 1
	}
	shardMaxEntries := normalized.MaxEntries / normalized.Shards
	if shardMaxEntries < 1 {
		shardMaxEntries = 1
	}
	for i := range c.shards {
		c.shards[i] = newShard(shardMaxBytes, shardMaxEntries)
	}
	c.mask = uint64(normalized.Shards - 1)
	if c.enabled {
		c.stopCh = make(chan struct{})
		c.doneCh = make(chan struct{})
		go c.sweeper()
	}
	return c, nil
}

// Get returns a copy of the cached value and whether it was present.
func (c *Cache) Get(key string) ([]byte, bool) {
	if c == nil || !c.enabled || c.closed.Load() {
		return nil, false
	}
	if !validKey(key) {
		c.stats.misses.Add(1)
		return nil, false
	}
	sh := c.shardFor(key)

	sh.mu.Lock()
	now := c.now()
	e := sh.entries[key]
	if e == nil {
		sh.mu.Unlock()
		c.stats.misses.Add(1)
		return nil, false
	}
	if expired(e, now) {
		sh.removeLocked(e)
		sh.mu.Unlock()
		c.stats.expired.Add(1)
		c.stats.misses.Add(1)
		return nil, false
	}
	sh.lru.MoveToFront(e.elem)
	value := e.value
	sh.mu.Unlock()

	c.stats.hits.Add(1)
	return cloneBytes(value), true
}

// Set stores a copy of value. A ttl of zero means Config.DefaultTTL. The value
// is silently ignored when the cache is disabled or closed.
func (c *Cache) Set(key string, value []byte, ttl time.Duration) error {
	if c == nil || !c.enabled || c.closed.Load() {
		return nil
	}
	if !validKey(key) {
		return ErrInvalidKey
	}
	resolvedTTL, err := c.resolveTTL(ttl)
	if err != nil {
		return err
	}
	if len(value) > c.cfg.MaxValueBytes {
		return ErrValueTooLarge
	}
	sh := c.shardFor(key)
	size := entrySize(key, len(value))
	if size > sh.maxBytes {
		return ErrValueTooLarge
	}
	stored := cloneBytes(value)
	expiresAt := c.now().Add(resolvedTTL)

	sh.mu.Lock()
	defer sh.mu.Unlock()
	if c.closed.Load() {
		return nil
	}
	if e := sh.entries[key]; e != nil {
		sh.bytes -= e.size
		e.value = stored
		e.size = size
		e.expiresAt = expiresAt
		sh.bytes += size
		sh.lru.MoveToFront(e.elem)
	} else {
		e := &entry{
			key:       key,
			value:     stored,
			expiresAt: expiresAt,
			size:      size,
		}
		e.elem = sh.lru.PushFront(e)
		sh.entries[key] = e
		sh.bytes += size
	}
	c.evictLocked(sh)
	return nil
}

// Delete removes keys and prevents in-flight loads from re-inserting a value
// read before the delete.
func (c *Cache) Delete(keys ...string) {
	if c == nil || !c.enabled || c.closed.Load() || len(keys) == 0 {
		return
	}
	c.epoch.Add(1)
	for _, key := range keys {
		if !validKey(key) {
			continue
		}
		sh := c.shardFor(key)
		sh.mu.Lock()
		if e := sh.entries[key]; e != nil {
			sh.removeLocked(e)
		}
		sh.mu.Unlock()
	}
}

// DeletePrefix removes every key with the given prefix. It scans all shards and
// is intended for infrequent group invalidation. An empty prefix is a no-op;
// use Flush to clear everything.
func (c *Cache) DeletePrefix(prefix string) {
	if c == nil || !c.enabled || c.closed.Load() || prefix == "" {
		return
	}
	c.epoch.Add(1)
	for _, sh := range c.shards {
		sh.mu.Lock()
		for key, e := range sh.entries {
			if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
				sh.removeLocked(e)
			}
		}
		sh.mu.Unlock()
	}
}

// Incr atomically adds delta to an integer value. A missing key starts at zero.
// The ttl rules match Set, except that ttl == 0 preserves an existing expiry and
// uses Config.DefaultTTL for a new key. A disabled cache returns 0, nil
// (fail-open); callers that need a hard limit must not rely on the cache alone.
func (c *Cache) Incr(key string, delta int64, ttl time.Duration) (int64, error) {
	if c == nil || !c.enabled || c.closed.Load() {
		return 0, nil
	}
	if !validKey(key) {
		return 0, ErrInvalidKey
	}
	resolvedTTL, err := c.resolveTTL(ttl)
	if err != nil {
		return 0, err
	}
	sh := c.shardFor(key)

	sh.mu.Lock()
	defer sh.mu.Unlock()
	if c.closed.Load() {
		return 0, nil
	}
	now := c.now()

	var current int64
	var expiresAt time.Time
	if e := sh.entries[key]; e != nil {
		if expired(e, now) {
			sh.removeLocked(e)
			c.stats.expired.Add(1)
		} else {
			value, parseErr := strconv.ParseInt(string(e.value), 10, 64)
			if parseErr != nil {
				return 0, ErrNotInteger
			}
			current = value
			if ttl == 0 {
				expiresAt = e.expiresAt
			}
		}
	}
	if expiresAt.IsZero() {
		expiresAt = now.Add(resolvedTTL)
	}
	next := current + delta
	raw := []byte(strconv.FormatInt(next, 10))
	size := entrySize(key, len(raw))
	if size > sh.maxBytes {
		return 0, ErrValueTooLarge
	}

	if e := sh.entries[key]; e != nil {
		sh.bytes -= e.size
		e.value = raw
		e.size = size
		e.expiresAt = expiresAt
		sh.bytes += size
		sh.lru.MoveToFront(e.elem)
	} else {
		e := &entry{key: key, value: raw, expiresAt: expiresAt, size: size}
		e.elem = sh.lru.PushFront(e)
		sh.entries[key] = e
		sh.bytes += size
	}
	c.evictLocked(sh)
	return next, nil
}

// GetOrLoad returns a cached value or loads it once for concurrent callers of
// the same key. Loader errors are returned to all waiters and are not cached.
func (c *Cache) GetOrLoad(ctx context.Context, key string, ttl time.Duration, load func(context.Context) ([]byte, error)) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if load == nil {
		return nil, ErrNilLoad
	}
	if c == nil || !c.enabled || c.closed.Load() {
		return load(ctx)
	}
	if !validKey(key) {
		return nil, ErrInvalidKey
	}
	if _, err := c.resolveTTL(ttl); err != nil {
		return nil, err
	}
	if value, ok := c.Get(key); ok {
		return value, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Capture the invalidation epoch before loading. If a Delete/DeletePrefix/
	// Flush happens while the loader runs, we must not re-insert the value that
	// may have been read before the invalidation.
	epoch := c.epoch.Load()
	value, err := c.flight.Do(ctx, key, func(loadCtx context.Context) ([]byte, error) {
		if cached, ok := c.Get(key); ok {
			return cached, nil
		}
		loadCtx, cancel := context.WithTimeout(context.WithoutCancel(loadCtx), c.cfg.LoadTimeout)
		defer cancel()

		started := time.Now()
		c.stats.loads.Add(1)
		loaded, loadErr := load(loadCtx)
		c.stats.loadNanos.Add(time.Since(started).Nanoseconds())
		if loadErr != nil {
			c.stats.loadErrors.Add(1)
			return nil, loadErr
		}
		if c.epoch.Load() == epoch {
			_ = c.Set(key, loaded, ttl)
		}
		return loaded, nil
	})
	if err != nil {
		return nil, err
	}
	return cloneBytes(value), nil
}

// Flush removes all entries but keeps configuration and cumulative counters.
func (c *Cache) Flush() {
	if c == nil || !c.enabled || c.closed.Load() {
		return
	}
	c.epoch.Add(1)
	for _, sh := range c.shards {
		sh.mu.Lock()
		sh.entries = make(map[string]*entry)
		sh.lru.Init()
		sh.bytes = 0
		sh.mu.Unlock()
	}
}

// Stats returns a snapshot of cache counters and current occupancy.
func (c *Cache) Stats() Stats {
	if c == nil {
		return Stats{}
	}
	stats := Stats{
		Enabled:      c.enabled && !c.closed.Load(),
		Hits:         c.stats.hits.Load(),
		Misses:       c.stats.misses.Load(),
		Evictions:    c.stats.evictions.Load(),
		Expired:      c.stats.expired.Load(),
		Loads:        c.stats.loads.Load(),
		LoadErrors:   c.stats.loadErrors.Load(),
		LoadDuration: time.Duration(c.stats.loadNanos.Load()),
	}
	for _, sh := range c.shards {
		sh.mu.Lock()
		stats.Entries += len(sh.entries)
		stats.Bytes += sh.bytes
		sh.mu.Unlock()
	}
	return stats
}

// Close stops the sweeper and releases cached entries. It is idempotent and
// safe to call on a nil cache.
func (c *Cache) Close() error {
	if c == nil {
		return nil
	}
	c.closeOnce.Do(func() {
		c.closed.Store(true)
		if c.stopCh != nil {
			close(c.stopCh)
			<-c.doneCh
		}
		for _, sh := range c.shards {
			sh.mu.Lock()
			sh.entries = make(map[string]*entry)
			sh.lru.Init()
			sh.bytes = 0
			sh.mu.Unlock()
		}
	})
	return nil
}

func (c *Cache) shardFor(key string) *shard {
	return c.shards[hashKey(key)&c.mask]
}

func (c *Cache) resolveTTL(ttl time.Duration) (time.Duration, error) {
	if ttl < 0 || ttl > c.cfg.MaxTTL {
		return 0, ErrInvalidTTL
	}
	if ttl == 0 {
		return c.cfg.DefaultTTL, nil
	}
	return ttl, nil
}

func (c *Cache) evictLocked(sh *shard) {
	for (sh.bytes > sh.maxBytes || len(sh.entries) > sh.maxEntries) && sh.lru.Len() > 0 {
		victim := sh.lru.Back()
		if victim == nil {
			return
		}
		sh.removeLocked(victim.Value.(*entry))
		c.stats.evictions.Add(1)
	}
}

func (c *Cache) sweeper() {
	defer close(c.doneCh)
	ticker := time.NewTicker(c.cfg.SweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			c.sweepOnce()
		case <-c.stopCh:
			return
		}
	}
}

func (c *Cache) sweepOnce() {
	now := c.now()
	for _, sh := range c.shards {
		sh.mu.Lock()
		for _, e := range sh.entries {
			if expired(e, now) {
				sh.removeLocked(e)
				c.stats.expired.Add(1)
			}
		}
		sh.mu.Unlock()
	}
}

func normalizeConfig(cfg Config) (Config, error) {
	if cfg.MaxBytes == 0 {
		cfg.MaxBytes = defaultMaxBytes
	}
	if cfg.MaxEntries == 0 {
		cfg.MaxEntries = defaultMaxEntries
	}
	if cfg.DefaultTTL == 0 {
		cfg.DefaultTTL = defaultDefaultTTL
	}
	if cfg.MaxTTL == 0 {
		cfg.MaxTTL = defaultMaxTTL
	}
	if cfg.MaxValueBytes == 0 {
		cfg.MaxValueBytes = defaultMaxValueBytes
		if int64(cfg.MaxValueBytes) > cfg.MaxBytes {
			cfg.MaxValueBytes = int(cfg.MaxBytes)
		}
	}
	if cfg.Shards == 0 {
		cfg.Shards = defaultShards
	}
	if cfg.SweepInterval == 0 {
		cfg.SweepInterval = defaultSweepInterval
	}
	if cfg.LoadTimeout == 0 {
		cfg.LoadTimeout = defaultLoadTimeout
	}
	if cfg.MaxBytes < 0 {
		return Config{}, fmt.Errorf("cache: MaxBytes must be positive")
	}
	if cfg.MaxBytes > 1<<30 {
		return Config{}, fmt.Errorf("cache: MaxBytes must not exceed 1 GiB")
	}
	if cfg.MaxEntries < 0 {
		return Config{}, fmt.Errorf("cache: MaxEntries must be positive")
	}
	if cfg.MaxEntries > 1_000_000 {
		return Config{}, fmt.Errorf("cache: MaxEntries must not exceed 1,000,000")
	}
	if cfg.DefaultTTL < 0 {
		return Config{}, fmt.Errorf("cache: DefaultTTL must be positive")
	}
	if cfg.MaxTTL <= 0 {
		return Config{}, fmt.Errorf("cache: MaxTTL must be positive")
	}
	if cfg.DefaultTTL > cfg.MaxTTL {
		return Config{}, fmt.Errorf("cache: DefaultTTL must not exceed MaxTTL")
	}
	if cfg.MaxValueBytes < 0 {
		return Config{}, fmt.Errorf("cache: MaxValueBytes must be positive")
	}
	if cfg.MaxValueBytes > int(cfg.MaxBytes) {
		return Config{}, fmt.Errorf("cache: MaxValueBytes must not exceed MaxBytes")
	}
	if cfg.Shards < 0 {
		return Config{}, fmt.Errorf("cache: Shards must be positive")
	}
	if cfg.SweepInterval < 0 {
		return Config{}, fmt.Errorf("cache: SweepInterval must be positive")
	}
	if cfg.LoadTimeout < 0 {
		return Config{}, fmt.Errorf("cache: LoadTimeout must be positive")
	}
	cfg.Shards = normalizeShards(cfg.Shards)
	return cfg, nil
}

func normalizeShards(shards int) int {
	if shards <= 1 {
		return 1
	}
	value := 1
	for value < shards && value < maxShards {
		value <<= 1
	}
	return value
}

func validKey(key string) bool {
	return len(key) > 0 && len(key) <= maxKeyLength && utf8.ValidString(key)
}

func hashKey(key string) uint64 {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)
	hash := uint64(offset64)
	for i := 0; i < len(key); i++ {
		hash ^= uint64(key[i])
		hash *= prime64
	}
	return hash
}

func entrySize(key string, valueLen int) int64 {
	return entryOverhead + int64(len(key)) + int64(valueLen)
}

func cloneBytes(value []byte) []byte {
	if len(value) == 0 {
		return nil
	}
	out := make([]byte, len(value))
	copy(out, value)
	return out
}
