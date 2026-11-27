package auth

import (
	"strings"
	"sync"
	"time"
)

const (
	// loginFailureWindow is the rolling window over which failed login attempts
	// are counted per client IP.
	loginFailureWindow = 1 * time.Hour
	// loginRateLimiterPruneThreshold is the map size at which expired entries
	// are opportunistically pruned.
	loginRateLimiterPruneThreshold = 4096
)

type loginAttempt struct {
	failures []time.Time
	lastSeen time.Time
}

// loginRateLimiter enforces a rolling 1-hour per-IP failed login limit.
// Both password logins and temporary credential logins share the same IP bucket
// to defend against password spraying and brute-force attacks.
type loginRateLimiter struct {
	mu       sync.Mutex
	attempts map[string]*loginAttempt
}

func newLoginRateLimiter() *loginRateLimiter {
	return &loginRateLimiter{attempts: make(map[string]*loginAttempt)}
}

func activeFailureTimes(times []time.Time, now time.Time) []time.Time {
	if len(times) == 0 {
		return nil
	}
	cutoff := now.Add(-loginFailureWindow)
	idx := 0
	for idx < len(times) && !times[idx].After(cutoff) {
		idx++
	}
	if idx == 0 {
		return times
	}
	if idx >= len(times) {
		return nil
	}
	kept := make([]time.Time, len(times)-idx)
	copy(kept, times[idx:])
	return kept
}

// allow reports whether a login attempt from key may proceed under maxFailures
// per rolling hour.
func (l *loginRateLimiter) allow(key string, now time.Time, maxFailures int) bool {
	if maxFailures < 1 {
		maxFailures = 1
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	attempt := l.attempts[key]
	if attempt == nil {
		return true
	}
	attempt.failures = activeFailureTimes(attempt.failures, now)
	if len(attempt.failures) == 0 {
		delete(l.attempts, key)
		return true
	}
	return len(attempt.failures) < maxFailures
}

func (l *loginRateLimiter) recordFailure(key string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.attempts) >= loginRateLimiterPruneThreshold {
		l.pruneLocked(now)
	}
	attempt := l.attempts[key]
	if attempt == nil {
		attempt = &loginAttempt{}
		l.attempts[key] = attempt
	}
	attempt.failures = activeFailureTimes(attempt.failures, now)
	attempt.failures = append(attempt.failures, now)
	attempt.lastSeen = now
}

func (l *loginRateLimiter) recordSuccess(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}

func (l *loginRateLimiter) pruneLocked(now time.Time) {
	for key, attempt := range l.attempts {
		attempt.failures = activeFailureTimes(attempt.failures, now)
		if len(attempt.failures) == 0 {
			delete(l.attempts, key)
		}
	}
}

func loginRateLimitKey(clientIP string) string {
	ip := strings.TrimSpace(clientIP)
	if ip == "" {
		return "unknown"
	}
	return ip
}
