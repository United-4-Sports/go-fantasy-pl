package client

import (
	"context"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fixedRateLimiter(capacity int, interval time.Duration) *rateLimiter {
	r := newRateLimiter(capacity, interval)
	now := time.Unix(0, 0)
	r.lastRefill = now
	r.clock = func() time.Time { return now }
	return r
}

// All time-based arithmetic assertions use injected time, not scheduler timing.
func TestRateLimiterRefill(t *testing.T) {
	r := fixedRateLimiter(3, 10*time.Nanosecond)
	start := r.lastRefill
	for range 3 {
		require.NoError(t, r.Wait(context.Background()))
	}
	require.Zero(t, r.tokens, "initial burst must be exactly capacity")
	require.Equal(t, 4*time.Nanosecond, r.nextDelay())

	r.refill(start.Add(2 * time.Nanosecond))
	assert.Zero(t, r.tokens)
	assert.Equal(t, uint64(6), r.credit)
	assert.Equal(t, 2*time.Nanosecond, r.nextDelay())
	r.refill(start.Add(4 * time.Nanosecond))
	assert.Equal(t, 1, r.tokens)
	assert.Equal(t, uint64(2), r.credit, "fractional remainder is retained")
	require.NoError(t, r.Wait(context.Background()))
	r.refill(start.Add(7 * time.Nanosecond))
	assert.Equal(t, 1, r.tokens)
	assert.Equal(t, uint64(1), r.credit)
	r.refill(start.Add(10 * time.Nanosecond))
	assert.Equal(t, 2, r.tokens)
	assert.Zero(t, r.credit)

	r.refill(start.Add(time.Hour))
	assert.Equal(t, 3, r.tokens, "multiple intervals and long idle cap at capacity")
	assert.Zero(t, r.credit)
	r.refill(start.Add(time.Hour + time.Nanosecond))
	assert.Zero(t, r.credit, "full bucket must not bank extra fractional credit")
}

func TestRateLimiterSustainedRate(t *testing.T) {
	r := fixedRateLimiter(50, time.Minute)
	now := r.lastRefill
	r.clock = func() time.Time { return now }
	for range 5 {
		for range 50 {
			require.NoError(t, r.Wait(context.Background()))
		}
		require.Zero(t, r.tokens)
		now = now.Add(600 * time.Millisecond)
		r.refill(now)
		require.Zero(t, r.tokens)
		now = now.Add(600 * time.Millisecond)
		r.refill(now)
		require.Equal(t, 1, r.tokens, "50/min earns a token every 1.2 seconds")
		now = now.Add(58800 * time.Millisecond)
		r.refill(now)
		require.Equal(t, 50, r.tokens)
	}
}

func TestRateLimiterExtremeArithmetic(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	maxDuration := time.Duration(1<<63 - 1)
	for _, tc := range []struct {
		name              string
		capacity          int
		interval, elapsed time.Duration
		credit            uint64
	}{
		{"sub-nanosecond rate", maxInt, 2, 1, 0},
		{"wide multiplication and carry", maxInt, maxDuration, maxDuration - 1, uint64(maxDuration - 1)},
		{"maximum delay", 1, maxDuration, 1, 0},
		{"minimum interval", maxInt, 1, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := fixedRateLimiter(tc.capacity, tc.interval)
			r.tokens, r.credit = 0, tc.credit
			delay := r.nextDelay()
			require.GreaterOrEqual(t, delay, time.Nanosecond)
			require.LessOrEqual(t, delay, tc.interval)
			numerator := new(big.Int).Mul(big.NewInt(int64(tc.elapsed)), big.NewInt(int64(tc.capacity)))
			numerator.Add(numerator, new(big.Int).SetUint64(tc.credit))
			earned, remainder := new(big.Int), new(big.Int)
			earned.QuoRem(numerator, big.NewInt(int64(tc.interval)), remainder)
			expected := int(earned.Int64())
			if expected >= tc.capacity {
				expected = tc.capacity
				remainder.SetInt64(0)
			}
			r.refill(r.lastRefill.Add(tc.elapsed))
			assert.Equal(t, expected, r.tokens)
			assert.Equal(t, remainder.Uint64(), r.credit)
		})
	}
}

func TestRateLimiterTimerWake(t *testing.T) {
	r := fixedRateLimiter(1, time.Nanosecond)
	r.tokens = 0
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	queued := 0
	r.waitHook = func() {
		queued++
		r.mu.Lock()
		r.refill(r.lastRefill.Add(time.Nanosecond))
		r.mu.Unlock()
	}
	require.NoError(t, r.Wait(ctx))
	assert.Equal(t, 1, queued, "fired timer must retry admission rather than spin")
	assert.Zero(t, r.tokens)
}

func TestRateLimiterConcurrentBudget(t *testing.T) {
	r := fixedRateLimiter(7, time.Hour)
	now := r.lastRefill
	r.clock = func() time.Time { return now }
	for round, budget := range []int{7, 3, 4} {
		if round > 0 {
			now = now.Add(30 * time.Minute)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		outcomes := make(chan bool, 64)
		r.waitHook = func() { outcomes <- false }
		var wg sync.WaitGroup
		for range 64 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				err := r.Wait(ctx)
				if err == nil {
					outcomes <- true
				} else {
					assert.ErrorIs(t, err, context.Canceled)
				}
			}()
		}
		admitted := 0
		for range 64 {
			select {
			case ok := <-outcomes:
				if ok {
					admitted++
				}
			case <-ctx.Done():
				cancel()
				wg.Wait()
				t.Fatal("callers failed to reach admission or queue barrier")
			}
		}
		cancel()
		wg.Wait()
		assert.Equal(t, budget, admitted, "only burst plus accrued credit may be consumed")
		assert.Zero(t, r.tokens)
	}
}
