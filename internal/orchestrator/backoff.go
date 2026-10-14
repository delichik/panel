package orchestrator

import (
	"math/rand/v2"
	"time"
)

const (
	baseRetryDelay = 30 * time.Second
	maxRetryDelay  = time.Hour
)

func retryDelay(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		if retryAfter > maxRetryDelay {
			return maxRetryDelay
		}
		return retryAfter
	}
	if attempt < 1 {
		attempt = 1
	}
	delay := baseRetryDelay
	for i := 1; i < attempt && delay < maxRetryDelay; i++ {
		delay *= 2
	}
	if delay > maxRetryDelay {
		delay = maxRetryDelay
	}
	// Jitter is bounded to ±20% and never changes the configured maximum.
	jitter := 0.8 + rand.Float64()*0.4
	result := time.Duration(float64(delay) * jitter)
	if result > maxRetryDelay {
		return maxRetryDelay
	}
	return result
}
