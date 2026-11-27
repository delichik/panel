package cache

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Unix(1_700_000_000, 0).UTC()}
}

func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *fakeClock) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}

func newTestCache(t *testing.T, cfg Config) (*Cache, *fakeClock) {
	t.Helper()
	clock := newFakeClock()
	c, err := newWithClock(cfg, clock.Now)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c, clock
}

func TestNewDefaults(t *testing.T) {
	c, err := New(Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer c.Close()

	if !c.enabled {
		t.Fatal("zero Config should enable the cache")
	}
	if c.cfg.MaxBytes != defaultMaxBytes {
		t.Fatalf("MaxBytes = %d, want %d", c.cfg.MaxBytes, defaultMaxBytes)
	}
	if c.cfg.MaxEntries != defaultMaxEntries {
		t.Fatalf("MaxEntries = %d, want %d", c.cfg.MaxEntries, defaultMaxEntries)
	}
	if c.cfg.DefaultTTL != defaultDefaultTTL {
		t.Fatalf("DefaultTTL = %v, want %v", c.cfg.DefaultTTL, defaultDefaultTTL)
	}
	if c.cfg.MaxTTL != defaultMaxTTL {
		t.Fatalf("MaxTTL = %v, want %v", c.cfg.MaxTTL, defaultMaxTTL)
	}
	if c.cfg.Shards != defaultShards {
		t.Fatalf("Shards = %d, want %d", c.cfg.Shards, defaultShards)
	}
	if c.mask != uint64(defaultShards-1) {
		t.Fatalf("mask = %d, want %d", c.mask, defaultShards-1)
	}
}

func TestNewValidation(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{name: "negative bytes", cfg: Config{MaxBytes: -1}},
		{name: "negative entries", cfg: Config{MaxEntries: -1}},
		{name: "default ttl above max", cfg: Config{DefaultTTL: 2 * time.Minute, MaxTTL: time.Minute}},
		{name: "negative default ttl", cfg: Config{DefaultTTL: -time.Second}},
		{name: "value above bytes", cfg: Config{MaxBytes: 1024, MaxValueBytes: 2048}},
		{name: "negative shards", cfg: Config{Shards: -1}},
		{name: "negative sweep", cfg: Config{SweepInterval: -time.Second}},
		{name: "negative load timeout", cfg: Config{LoadTimeout: -time.Second}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := New(tt.cfg); err == nil {
				t.Fatal("New() error = nil, want validation error")
			}
		})
	}

	c, err := New(Config{Shards: 3})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer c.Close()
	if c.cfg.Shards != 4 {
		t.Fatalf("Shards = %d, want rounded up 4", c.cfg.Shards)
	}

	c2, err := New(Config{Shards: 1000})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer c2.Close()
	if c2.cfg.Shards != maxShards {
		t.Fatalf("Shards = %d, want capped %d", c2.cfg.Shards, maxShards)
	}
}

func TestSetGetCopySemantics(t *testing.T) {
	c, _ := newTestCache(t, Config{DefaultTTL: 10 * time.Second})

	original := []byte("abc")
	if err := c.Set("k", original, 0); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	original[0] = 'z'

	got, ok := c.Get("k")
	if !ok || string(got) != "abc" {
		t.Fatalf("Get() = %q, %v, want abc, true", got, ok)
	}
	got[0] = 'x'
	again, ok := c.Get("k")
	if !ok || string(again) != "abc" {
		t.Fatalf("Get() after mutating result = %q, %v, want abc, true", again, ok)
	}

	if err := c.Set("k", []byte("next"), 0); err != nil {
		t.Fatalf("overwrite Set() error = %v", err)
	}
	if got, _ := c.Get("k"); string(got) != "next" {
		t.Fatalf("Get() after overwrite = %q, want next", got)
	}

	if err := c.Set("empty", nil, 0); err != nil {
		t.Fatalf("Set(empty) error = %v", err)
	}
	if got, ok := c.Get("empty"); !ok || len(got) != 0 {
		t.Fatalf("Get(empty) = %q, %v, want empty, true", got, ok)
	}
}

func TestTTL(t *testing.T) {
	c, clock := newTestCache(t, Config{DefaultTTL: 2 * time.Second})

	if err := c.Set("k", []byte("v"), 0); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	clock.Advance(1999 * time.Millisecond)
	if _, ok := c.Get("k"); !ok {
		t.Fatal("entry should still be alive before default TTL")
	}
	clock.Advance(2 * time.Millisecond)
	if _, ok := c.Get("k"); ok {
		t.Fatal("entry should be expired after default TTL")
	}
	if got := c.Stats().Expired; got != 1 {
		t.Fatalf("Expired = %d, want 1", got)
	}

	if err := c.Set("explicit", []byte("v"), 5*time.Second); err != nil {
		t.Fatalf("Set(explicit) error = %v", err)
	}
	clock.Advance(4 * time.Second)
	if _, ok := c.Get("explicit"); !ok {
		t.Fatal("entry should be alive before explicit TTL")
	}
	clock.Advance(2 * time.Second)
	if _, ok := c.Get("explicit"); ok {
		t.Fatal("entry should be expired after explicit TTL")
	}

	if err := c.Set("bad", []byte("v"), -time.Second); !errors.Is(err, ErrInvalidTTL) {
		t.Fatalf("Set(negative ttl) error = %v, want ErrInvalidTTL", err)
	}
	if err := c.Set("bad", []byte("v"), 61*time.Second); !errors.Is(err, ErrInvalidTTL) {
		t.Fatalf("Set(ttl above max) error = %v, want ErrInvalidTTL", err)
	}
}

func TestLRUEviction(t *testing.T) {
	c, _ := newTestCache(t, Config{
		MaxBytes:      2048,
		MaxEntries:    1000,
		MaxValueBytes: 1024,
		Shards:        1,
	})
	value := make([]byte, 100)
	for i := 0; i < 20; i++ {
		key := "k" + string(rune('a'+i))
		if err := c.Set(key, value, time.Minute); err != nil {
			t.Fatalf("Set(%s) error = %v", key, err)
		}
	}
	if _, ok := c.Get("ka"); ok {
		t.Fatal("oldest entry should have been evicted")
	}
	if _, ok := c.Get("kt"); !ok {
		t.Fatal("newest entry should still be cached")
	}
	stats := c.Stats()
	if stats.Evictions == 0 {
		t.Fatal("expected evictions")
	}
	if stats.Bytes > 2048 {
		t.Fatalf("Bytes = %d, want <= 2048", stats.Bytes)
	}
}

func TestMaxEntriesEviction(t *testing.T) {
	c, _ := newTestCache(t, Config{
		MaxBytes:   1 << 20,
		MaxEntries: 2,
		Shards:     1,
	})
	for _, key := range []string{"a", "b", "c"} {
		if err := c.Set(key, []byte("v"), time.Second); err != nil {
			t.Fatalf("Set(%s) error = %v", key, err)
		}
	}
	if _, ok := c.Get("a"); ok {
		t.Fatal("oldest entry should be evicted by MaxEntries")
	}
	if _, ok := c.Get("b"); !ok {
		t.Fatal("b should be cached")
	}
	if _, ok := c.Get("c"); !ok {
		t.Fatal("c should be cached")
	}
}

func TestMaxValueBytes(t *testing.T) {
	c, _ := newTestCache(t, Config{
		MaxBytes:      4096,
		MaxValueBytes: 100,
		Shards:        1,
	})
	if err := c.Set("big", make([]byte, 101), time.Second); !errors.Is(err, ErrValueTooLarge) {
		t.Fatalf("Set(too large) error = %v, want ErrValueTooLarge", err)
	}
	if err := c.Set("ok", make([]byte, 100), time.Second); err != nil {
		t.Fatalf("Set(100 bytes) error = %v", err)
	}
}

func TestDeleteAndDeletePrefix(t *testing.T) {
	c, _ := newTestCache(t, Config{DefaultTTL: 10 * time.Second})
	for _, key := range []string{"a:1", "a:2", "b:1"} {
		if err := c.Set(key, []byte(key), 0); err != nil {
			t.Fatalf("Set(%s) error = %v", key, err)
		}
	}

	c.DeletePrefix("a:")
	if _, ok := c.Get("a:1"); ok {
		t.Fatal("a:1 should be removed by prefix")
	}
	if _, ok := c.Get("a:2"); ok {
		t.Fatal("a:2 should be removed by prefix")
	}
	if got, ok := c.Get("b:1"); !ok || string(got) != "b:1" {
		t.Fatalf("Get(b:1) = %q, %v, want b:1, true", got, ok)
	}

	c.DeletePrefix("")
	if _, ok := c.Get("b:1"); !ok {
		t.Fatal("empty prefix must be a no-op")
	}

	c.Delete("b:1")
	if _, ok := c.Get("b:1"); ok {
		t.Fatal("Delete should remove the key")
	}
}

func TestIncr(t *testing.T) {
	c, clock := newTestCache(t, Config{DefaultTTL: 30 * time.Second})

	if got, err := c.Incr("n", 1, 0); err != nil || got != 1 {
		t.Fatalf("Incr() = %d, %v, want 1, nil", got, err)
	}
	if got, err := c.Incr("n", 10, 0); err != nil || got != 11 {
		t.Fatalf("Incr() = %d, %v, want 11, nil", got, err)
	}
	if got, _ := c.Get("n"); string(got) != "11" {
		t.Fatalf("Get(n) = %q, want 11", got)
	}

	if err := c.Set("text", []byte("abc"), 0); err != nil {
		t.Fatalf("Set(text) error = %v", err)
	}
	if _, err := c.Incr("text", 1, 0); !errors.Is(err, ErrNotInteger) {
		t.Fatalf("Incr(text) error = %v, want ErrNotInteger", err)
	}

	// ttl == 0 preserves an existing expiry.
	if _, err := c.Incr("timed", 1, time.Second); err != nil {
		t.Fatalf("Incr(timed) error = %v", err)
	}
	clock.Advance(500 * time.Millisecond)
	if _, err := c.Incr("timed", 1, 0); err != nil {
		t.Fatalf("Incr(timed, ttl=0) error = %v", err)
	}
	clock.Advance(600 * time.Millisecond)
	if _, ok := c.Get("timed"); ok {
		t.Fatal("Incr must preserve the existing expiry")
	}

	// Concurrent increments must not lose updates.
	var wg sync.WaitGroup
	const workers = 50
	const perWorker = 20
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perWorker; j++ {
				if _, err := c.Incr("counter", 1, 0); err != nil {
					t.Errorf("Incr(counter) error = %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
	got, err := c.Incr("counter", 0, 0)
	if err != nil {
		t.Fatalf("Incr(counter, 0) error = %v", err)
	}
	if got != workers*perWorker {
		t.Fatalf("counter = %d, want %d", got, workers*perWorker)
	}
}

func TestGetOrLoadSingleFlight(t *testing.T) {
	c, _ := newTestCache(t, Config{DefaultTTL: 2 * time.Second})
	var loads atomic.Int32
	release := make(chan struct{})
	started := make(chan struct{})

	const callers = 50
	var wg sync.WaitGroup
	results := make([]string, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			value, err := c.GetOrLoad(context.Background(), "k", 0, func(context.Context) ([]byte, error) {
				if loads.Add(1) == 1 {
					close(started)
				}
				<-release
				return []byte("value"), nil
			})
			if err != nil {
				t.Errorf("GetOrLoad() error = %v", err)
				return
			}
			results[index] = string(value)
		}(i)
	}
	<-started
	time.Sleep(50 * time.Millisecond) // let the other callers join the flight
	close(release)
	wg.Wait()

	if got := loads.Load(); got != 1 {
		t.Fatalf("loads = %d, want 1", got)
	}
	for i, result := range results {
		if result != "value" {
			t.Fatalf("result[%d] = %q, want value", i, result)
		}
	}
	if got := c.Stats().Loads; got != 1 {
		t.Fatalf("Stats.Loads = %d, want 1", got)
	}
}

func TestGetOrLoadErrorsAreNotCached(t *testing.T) {
	c, _ := newTestCache(t, Config{DefaultTTL: 2 * time.Second})
	loadErr := errors.New("boom")
	var calls int
	if _, err := c.GetOrLoad(context.Background(), "k", 0, func(context.Context) ([]byte, error) {
		calls++
		return nil, loadErr
	}); !errors.Is(err, loadErr) {
		t.Fatalf("GetOrLoad() error = %v, want %v", err, loadErr)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
	if _, ok := c.Get("k"); ok {
		t.Fatal("an error must not be cached")
	}
	value, err := c.GetOrLoad(context.Background(), "k", 0, func(context.Context) ([]byte, error) {
		calls++
		return []byte("ok"), nil
	})
	if err != nil || string(value) != "ok" {
		t.Fatalf("second GetOrLoad() = %q, %v, want ok, nil", value, err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	if got := c.Stats().LoadErrors; got != 1 {
		t.Fatalf("Stats.LoadErrors = %d, want 1", got)
	}
}

func TestGetOrLoadRecoversPanic(t *testing.T) {
	c, _ := newTestCache(t, Config{DefaultTTL: 2 * time.Second})
	if _, err := c.GetOrLoad(context.Background(), "k", 0, func(context.Context) ([]byte, error) {
		panic("boom")
	}); err == nil {
		t.Fatal("GetOrLoad() error = nil, want panic error")
	}
	value, err := c.GetOrLoad(context.Background(), "k", 0, func(context.Context) ([]byte, error) {
		return []byte("ok"), nil
	})
	if err != nil || string(value) != "ok" {
		t.Fatalf("GetOrLoad() after panic = %q, %v, want ok, nil", value, err)
	}
}

func TestGetOrLoadCanceledWaiter(t *testing.T) {
	c, _ := newTestCache(t, Config{DefaultTTL: 2 * time.Second})
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = c.GetOrLoad(context.Background(), "k", 0, func(context.Context) ([]byte, error) {
			close(started)
			<-release
			return []byte("value"), nil
		})
	}()
	<-started

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.GetOrLoad(ctx, "k", 0, func(context.Context) ([]byte, error) {
		t.Error("waiter must not run the loader")
		return nil, nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("GetOrLoad(canceled) error = %v, want context.Canceled", err)
	}
	close(release)
	<-done
}

func TestDeleteDuringLoadDoesNotReinsert(t *testing.T) {
	c, _ := newTestCache(t, Config{DefaultTTL: 2 * time.Second})
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = c.GetOrLoad(context.Background(), "k", 0, func(context.Context) ([]byte, error) {
			close(started)
			<-release
			return []byte("old"), nil
		})
	}()
	<-started
	c.Delete("k")
	close(release)
	<-done

	if value, ok := c.Get("k"); ok {
		t.Fatalf("Get() = %q, true after Delete during load, want miss", value)
	}
}

func TestGetOrLoadInvalidTTL(t *testing.T) {
	c, _ := newTestCache(t, Config{DefaultTTL: 2 * time.Second})
	var called bool
	if _, err := c.GetOrLoad(context.Background(), "k", -time.Second, func(context.Context) ([]byte, error) {
		called = true
		return nil, nil
	}); !errors.Is(err, ErrInvalidTTL) {
		t.Fatalf("GetOrLoad() error = %v, want ErrInvalidTTL", err)
	}
	if called {
		t.Fatal("loader must not run for an invalid TTL")
	}
}

func TestFlushAndClose(t *testing.T) {
	c, _ := newTestCache(t, Config{DefaultTTL: 10 * time.Second})
	if err := c.Set("a", []byte("1"), 0); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	c.Flush()
	if _, ok := c.Get("a"); ok {
		t.Fatal("Flush should remove entries")
	}
	if stats := c.Stats(); stats.Entries != 0 || stats.Bytes != 0 {
		t.Fatalf("stats after flush = %+v", stats)
	}

	if err := c.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if err := c.Set("b", []byte("2"), 0); err != nil {
		t.Fatalf("Set after Close() error = %v", err)
	}
	if _, ok := c.Get("b"); ok {
		t.Fatal("closed cache must not serve values")
	}
	if c.Stats().Enabled {
		t.Fatal("closed cache must report Enabled=false")
	}
}

func TestDisabledAndNil(t *testing.T) {
	c, err := New(Config{Disabled: true})
	if err != nil {
		t.Fatalf("New(disabled) error = %v", err)
	}
	if c.Stats().Enabled {
		t.Fatal("disabled cache must report Enabled=false")
	}
	if err := c.Set("k", []byte("v"), time.Second); err != nil {
		t.Fatalf("Set(disabled) error = %v", err)
	}
	if _, ok := c.Get("k"); ok {
		t.Fatal("disabled cache must miss")
	}
	value, err := c.GetOrLoad(context.Background(), "k", time.Second, func(context.Context) ([]byte, error) {
		return []byte("loaded"), nil
	})
	if err != nil || string(value) != "loaded" {
		t.Fatalf("GetOrLoad(disabled) = %q, %v, want loaded, nil", value, err)
	}
	c.Delete("k")
	c.DeletePrefix("k")
	c.Flush()
	if err := c.Close(); err != nil {
		t.Fatalf("Close(disabled) error = %v", err)
	}

	var nilCache *Cache
	if _, ok := nilCache.Get("k"); ok {
		t.Fatal("nil cache Get must miss")
	}
	if err := nilCache.Set("k", []byte("v"), time.Second); err != nil {
		t.Fatalf("nil cache Set error = %v", err)
	}
	nilCache.Delete("k")
	nilCache.DeletePrefix("k")
	nilCache.Flush()
	if stats := nilCache.Stats(); stats.Enabled || stats.Entries != 0 {
		t.Fatalf("nil cache Stats = %+v", stats)
	}
	value, err = nilCache.GetOrLoad(context.Background(), "k", time.Second, func(context.Context) ([]byte, error) {
		return []byte("nil-loaded"), nil
	})
	if err != nil || string(value) != "nil-loaded" {
		t.Fatalf("nil cache GetOrLoad = %q, %v, want nil-loaded, nil", value, err)
	}
	if err := nilCache.Close(); err != nil {
		t.Fatalf("nil cache Close error = %v", err)
	}
}

func TestStatsAndSweep(t *testing.T) {
	c, clock := newTestCache(t, Config{DefaultTTL: time.Second, Shards: 1})
	if err := c.Set("a", []byte("123"), 0); err != nil {
		t.Fatalf("Set(a) error = %v", err)
	}
	if _, ok := c.Get("a"); !ok {
		t.Fatal("expected hit")
	}
	if _, ok := c.Get("missing"); ok {
		t.Fatal("expected miss")
	}
	stats := c.Stats()
	if stats.Hits != 1 || stats.Misses != 1 {
		t.Fatalf("stats = %+v, want 1 hit / 1 miss", stats)
	}
	wantBytes := entrySize("a", 3)
	if stats.Entries != 1 || stats.Bytes != wantBytes {
		t.Fatalf("stats entries/bytes = %d/%d, want 1/%d", stats.Entries, stats.Bytes, wantBytes)
	}

	if err := c.Set("b", []byte("456"), 10*time.Second); err != nil {
		t.Fatalf("Set(b) error = %v", err)
	}
	clock.Advance(2 * time.Second)
	c.sweepOnce()
	if _, ok := c.Get("a"); ok {
		t.Fatal("swept entry should be gone")
	}
	if _, ok := c.Get("b"); !ok {
		t.Fatal("unexpired entry should survive the sweep")
	}
}

func TestKeyValidation(t *testing.T) {
	c, _ := newTestCache(t, Config{DefaultTTL: time.Second})
	if err := c.Set("", []byte("v"), 0); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("Set(empty) error = %v, want ErrInvalidKey", err)
	}
	longKey := strings.Repeat("k", 513)
	if err := c.Set(longKey, []byte("v"), 0); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("Set(long) error = %v, want ErrInvalidKey", err)
	}
	invalidUTF8 := string([]byte{0xff})
	if err := c.Set(invalidUTF8, []byte("v"), 0); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("Set(invalid utf8) error = %v, want ErrInvalidKey", err)
	}
	if _, ok := c.Get(""); ok {
		t.Fatal("Get(empty) must miss")
	}
}

func TestJSONHelpers(t *testing.T) {
	type payload struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	c, _ := newTestCache(t, Config{DefaultTTL: time.Second})

	if err := SetJSON(c, "p", payload{Name: "x", Count: 2}, 0); err != nil {
		t.Fatalf("SetJSON() error = %v", err)
	}
	got, ok := GetJSON[payload](c, "p")
	if !ok || got != (payload{Name: "x", Count: 2}) {
		t.Fatalf("GetJSON() = %+v, %v", got, ok)
	}
	if _, ok := GetJSON[payload](c, "missing"); ok {
		t.Fatal("GetJSON(missing) should miss")
	}

	if err := c.Set("broken", []byte("{"), 0); err != nil {
		t.Fatalf("Set(broken) error = %v", err)
	}
	if _, ok := GetJSON[payload](c, "broken"); ok {
		t.Fatal("GetJSON(broken) should miss")
	}

	loaded, err := GetOrLoadJSON(context.Background(), c, "loaded", 0, func(context.Context) (payload, error) {
		return payload{Name: "y", Count: 3}, nil
	})
	if err != nil || loaded != (payload{Name: "y", Count: 3}) {
		t.Fatalf("GetOrLoadJSON() = %+v, %v", loaded, err)
	}
}
