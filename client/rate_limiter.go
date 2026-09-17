package client

import (
	"context"
	"math/bits"
	"sync"
	"time"
)

// rateLimiter is a per-client, thread-safe token bucket. Capacity N starts full
// and earns elapsed*N/interval tokens, retaining fractional credit up to N tokens.
type rateLimiter struct {
	mu         sync.Mutex
	tokens     int
	maxTokens  int
	interval   time.Duration
	credit     uint64 // fractional token numerator; denominator is interval in ns
	lastRefill time.Time
	clock      func() time.Time
	waitHook   func() // optional queue barrier; set before use, called without mu
}

// newRateLimiter requires positive capacity and interval, validated by WithRateLimit.
func newRateLimiter(maxTokens int, interval time.Duration) *rateLimiter {
	return &rateLimiter{
		tokens: maxTokens, maxTokens: maxTokens, interval: interval,
		lastRefill: time.Now(), clock: time.Now,
	}
}

// refill credits elapsed*N/interval tokens without losing fractional credit.
// Caller holds mu.
func (r *rateLimiter) refill(now time.Time) {
	elapsed := now.Sub(r.lastRefill)
	if elapsed <= 0 {
		return
	}
	r.lastRefill = now
	if elapsed >= r.interval || r.tokens == r.maxTokens {
		r.tokens, r.credit = r.maxTokens, 0
		return
	}
	// A 128-bit intermediate avoids overflow for large capacities/durations.
	// elapsed < interval guarantees the quotient fits in uint64.
	hi, lo := bits.Mul64(uint64(elapsed), uint64(r.maxTokens))
	lo, carry := bits.Add64(lo, r.credit, 0)
	hi += carry
	earned, credit := bits.Div64(hi, lo, uint64(r.interval))
	if earned >= uint64(r.maxTokens-r.tokens) {
		r.tokens, r.credit = r.maxTokens, 0
		return
	}
	r.tokens += int(earned)
	r.credit = credit
}

// nextDelay returns ceil((interval-credit)/N), at least one nanosecond.
// Caller holds mu and the bucket is empty. Division avoids overflow and
// sub-nanosecond rates never turn into zero-delay spinning.
func (r *rateLimiter) nextDelay() time.Duration {
	remaining, capacity := uint64(r.interval)-r.credit, uint64(r.maxTokens)
	delay := remaining / capacity
	if remaining%capacity != 0 {
		delay++
	}
	return time.Duration(delay)
}

// Wait waits without reserving a token or holding mu while blocked. Cancellation
// observed before admission consumes no token. Concurrent waiters recheck the
// bucket on waking; no fairness guarantee or background ticker is provided.
func (r *rateLimiter) Wait(ctx context.Context) error {
	for {
		r.mu.Lock()
		if err := ctx.Err(); err != nil {
			r.mu.Unlock()
			return err
		}
		r.refill(r.clock())
		if r.tokens > 0 {
			r.tokens--
			r.mu.Unlock()
			return nil
		}
		delay := r.nextDelay()
		r.mu.Unlock()

		timer := time.NewTimer(delay)
		if r.waitHook != nil {
			r.waitHook()
		}
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return ctx.Err()
		case <-timer.C:
			// The fired timer is drained; the next iteration rechecks cancellation.
		}
	}
}
