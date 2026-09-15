package auth

import (
	"sync"
	"time"
)

const (
	// loginMaxFailures is the number of consecutive failed attempts that
	// triggers a lockout for a (client IP, username) pair.
	loginMaxFailures = 5
	// loginLockoutWindow is how long a locked pair stays locked before the
	// counter is reset.
	loginLockoutWindow = 15 * time.Minute
	// loginRetentionWindow drops idle entries after this long so the in-memory
	// table cannot grow without bound.
	loginRetentionWindow = 1 * time.Hour
	// loginRateLimiterPruneThreshold is the map size at which expired entries
	// are opportunistically pruned.
	loginRateLimiterPruneThreshold = 4096
)

type loginAttempt struct {
	failures    int
	lockedUntil time.Time
	lastSeen    time.Time
}

// loginRateLimiter is a small in-memory failure limiter keyed by
// "clientIP|username". It counts consecutive failures and temporarily locks
// the key after loginMaxFailures; a successful login clears the entry.
type loginRateLimiter struct {
	mu       sync.Mutex
	attempts map[string]*loginAttempt
}

func newLoginRateLimiter() *loginRateLimiter {
	return &loginRateLimiter{attempts: make(map[string]*loginAttempt)}
}

// allow reports whether a login attempt for key may proceed. A key whose
// lockout has expired is reset so one more typo does not instantly re-lock.
func (l *loginRateLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	attempt := l.attempts[key]
	if attempt == nil {
		return true
	}
	if now.Before(attempt.lockedUntil) {
		return false
	}
	if attempt.failures >= loginMaxFailures || now.Sub(attempt.lastSeen) > loginRetentionWindow {
		delete(l.attempts, key)
	}
	return true
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
	attempt.failures++
	attempt.lastSeen = now
	if attempt.failures >= loginMaxFailures {
		attempt.lockedUntil = now.Add(loginLockoutWindow)
	}
}

func (l *loginRateLimiter) recordSuccess(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}

func (l *loginRateLimiter) pruneLocked(now time.Time) {
	for key, attempt := range l.attempts {
		if !now.Before(attempt.lockedUntil) || now.Sub(attempt.lastSeen) > loginRetentionWindow {
			delete(l.attempts, key)
		}
	}
}

func loginRateLimitKey(clientIP, username string) string {
	return clientIP + "|" + username
}
