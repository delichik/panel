// Package cache provides a small, in-process, Redis-like cache with TTL, LRU
// eviction, bounded memory and single-flight loading.
//
// The cache is deliberately independent of the database, ORM and business
// packages. It does not know when data changes and does not invalidate entries
// automatically. Callers own the key design, the TTL and the invalidation
// points:
//
//	value, err := cache.GetOrLoadJSON(ctx, c, "facility:metrics:config",
//	    2*time.Second,
//	    func(ctx context.Context) (Config, error) {
//	        return service.loadConfig(ctx)
//	    })
//
//	// after a successful write
//	c.Delete("facility:metrics:config")
//
// The cache is intended for short-lived read deduplication (the default TTL is
// two seconds and the hard limit is one minute), not for long-lived data
// storage. Correctness must never depend on a cache hit: misses, eviction,
// disabling and process restarts must all fall back to the source of truth.
//
// The zero Config is usable and enables the cache with defaults. Use
// Config.Disabled to get a safe no-op instance.
package cache
