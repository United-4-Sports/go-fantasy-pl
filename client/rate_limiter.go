package client

import (
	"sync"
	"time"
)

// rateLimiter implements a token bucket algorithm to control the rate of outgoing API requests.
// It is thread-safe and can be shared across multiple goroutines.
type rateLimiter struct {
	tokens     int
	maxTokens  int
	interval   time.Duration
	lastRefill time.Time
	mu         sync.Mutex
}

// newRateLimiter initializes a new rateLimiter with the specified capacity and refill interval.
func newRateLimiter(maxTokens int, interval time.Duration) *rateLimiter {
	return &rateLimiter{
		tokens:     maxTokens,
		maxTokens:  maxTokens,
		interval:   interval,
		lastRefill: time.Now(),
	}
}

// Wait blocks the calling goroutine until a token becomes available in the bucket.
// It automatically refills tokens based on the elapsed time since the last refill.
func (r *rateLimiter) Wait() {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(r.lastRefill)
	refillTokens := int(elapsed / r.interval)

	if refillTokens > 0 {
		r.tokens = min(r.maxTokens, r.tokens+refillTokens)
		r.lastRefill = now
	}

	for r.tokens <= 0 {
		r.mu.Unlock()          // Unlock before sleeping to allow other goroutines to proceed
		time.Sleep(r.interval) // Sleep for the interval
		r.mu.Lock()            // Re-lock after sleeping

		// Recheck the token count after waking up
		elapsed = time.Since(r.lastRefill)
		refillTokens = int(elapsed / r.interval)
		if refillTokens > 0 {
			r.tokens = min(r.maxTokens, r.tokens+refillTokens)
			r.lastRefill = time.Now()
		}
	}

	// Consume a token
	r.tokens--
}
