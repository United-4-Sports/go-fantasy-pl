package client

import (
	"context"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"
)

// Default throttle policy values, resolved when a field is left at zero.
const (
	default429Retries    = 1
	default403Retries    = 2
	defaultBaseBackoff   = 500 * time.Millisecond
	defaultMaxBackoff    = 8 * time.Second
	defaultRetryAfterCap = 2 * time.Minute

	maxRetryAfterDuration = time.Duration(1<<63 - 1)
	maxRetryAfterSeconds  = uint64(maxRetryAfterDuration / time.Second)
)

// ThrottlePolicy controls the client's automatic handling of throttled
// responses: 429 Too Many Requests and 403 Forbidden, which the FPL API
// uses heuristically to block heavy clients.
//
// A 429 with a Retry-After header (delay-seconds or HTTP-date) is waited
// out exactly as asked, capped at RetryAfterCap, and retried. Without the
// header — and always for 403 — the client backs off exponentially with
// jitter: the nth retry waits BaseBackoff * 2^(n-1), halved-to-1.5x
// randomized, capped at MaxBackoff.
//
// Each retry is a full attempt through the rate limiter, so retries
// consume limiter tokens like any other request.
//
// The zero value means "all defaults". Individual retry classes can be
// turned off by setting their count to -1; the whole mechanism (including
// ThrottleEvent emission) is off when Disabled is true.
type ThrottlePolicy struct {
	// Disabled turns all throttle handling off: throttled responses are
	// returned to the caller on the first attempt, exactly as in SDK
	// versions before this existed.
	Disabled bool

	// Max429Retries is how many times a 429 response is retried.
	// 0 means the default (1); -1 disables 429 retries.
	Max429Retries int

	// Max403Retries is how many times a throttle-suspected 403 response
	// is retried. 0 means the default (2); -1 disables 403 retries.
	Max403Retries int

	// BaseBackoff is the first 403 backoff delay (before jitter).
	// Non-positive values use the 500ms default.
	BaseBackoff time.Duration

	// MaxBackoff bounds any single backoff wait. Non-positive values use
	// the 8s default; it is never resolved below BaseBackoff.
	MaxBackoff time.Duration

	// RetryAfterCap bounds how long a server-provided Retry-After is
	// honored. Non-positive values use the 2 minute default. A caller
	// context deadline still bounds the whole operation.
	RetryAfterCap time.Duration
}

// DefaultThrottlePolicy returns the policy applied when none is set:
// one 429 retry honoring Retry-After, two 403 retries with exponential
// backoff and jitter, base 500ms capped at 8s.
func DefaultThrottlePolicy() ThrottlePolicy {
	return ThrottlePolicy{
		Max429Retries: default429Retries,
		Max403Retries: default403Retries,
		BaseBackoff:   defaultBaseBackoff,
		MaxBackoff:    defaultMaxBackoff,
		RetryAfterCap: defaultRetryAfterCap,
	}
}

// resolved fills in defaults for unset fields: 0 means default, -1 means
// "never retry this class" and resolves to zero attempts.
func (p ThrottlePolicy) resolved() ThrottlePolicy {
	if p.Max429Retries == 0 {
		p.Max429Retries = default429Retries
	}
	if p.Max429Retries < 0 {
		p.Max429Retries = 0
	}
	if p.Max403Retries == 0 {
		p.Max403Retries = default403Retries
	}
	if p.Max403Retries < 0 {
		p.Max403Retries = 0
	}
	if p.BaseBackoff <= 0 {
		p.BaseBackoff = defaultBaseBackoff
	}
	if p.MaxBackoff <= 0 {
		p.MaxBackoff = defaultMaxBackoff
	}
	if p.MaxBackoff < p.BaseBackoff {
		p.MaxBackoff = p.BaseBackoff
	}
	if p.RetryAfterCap <= 0 {
		p.RetryAfterCap = defaultRetryAfterCap
	}
	return p
}

// ThrottleEvent reports one throttled response to the observer installed
// with WithThrottleObserver. The observer is called synchronously on the
// request path and must be nonblocking; it never receives response bodies.
type ThrottleEvent struct {
	// Endpoint is the API path that was throttled.
	Endpoint string
	// StatusCode is 429 or 403.
	StatusCode int
	// Attempt is the 1-based number of the attempt that was throttled.
	Attempt int
	// Wait is the delay the client will sleep before the next attempt,
	// after jitter and caps. Zero when WillRetry is false.
	Wait time.Duration
	// RetryAfter is the parsed Retry-After value the server sent, if any.
	// Numeric values too large to represent as a time.Duration are saturated
	// to the maximum time.Duration (MaxInt64 nanoseconds).
	RetryAfter time.Duration
	// WillRetry is false when the retry budget for this status class is
	// exhausted and the throttled response is being surfaced to the caller.
	WillRetry bool
}

// throttleable reports whether a status receives automatic backoff.
func throttleable(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests || statusCode == http.StatusForbidden
}

// backoff computes the exponential delay for the nth retry of a class,
// randomized to [0.5x, 1.5x) of the nominal value and capped at max.
func backoff(base, max time.Duration, retryN int, jitter func() float64) time.Duration {
	shift := min(retryN-1, 30)
	nominal := base << shift // << on Duration shifts nanoseconds; overflow goes negative
	if nominal <= 0 || nominal > max {
		nominal = max
	}
	scaled := time.Duration(float64(nominal) * (0.5 + jitter()))
	if scaled < 0 {
		return 0
	}
	if scaled > max {
		scaled = max
	}
	return scaled
}

// parseRetryAfter parses the Retry-After header in both wire formats:
// delay-seconds and HTTP-date (resolved relative to now). Past dates
// resolve to zero. Numeric values that exceed time.Duration are saturated
// to its maximum value. ok is false when the header is absent or unparseable.
func parseRetryAfter(h http.Header, now time.Time) (d time.Duration, ok bool) {
	v := h.Get("Retry-After")
	if v == "" {
		return 0, false
	}
	if decimalDigits(v) {
		secs, err := strconv.ParseUint(v, 10, 64)
		if err != nil || secs > maxRetryAfterSeconds {
			return maxRetryAfterDuration, true
		}
		return time.Duration(secs) * time.Second, true
	}
	if date, err := http.ParseTime(v); err == nil {
		delay := max(date.Sub(now), 0)
		return delay, true
	}
	return 0, false
}

// decimalDigits reports whether v is a non-empty sequence of ASCII digits.
// ParseUint can report ErrRange before it examines a later invalid byte, so
// validate the complete wire value before treating a range error as numeric
// overflow.
func decimalDigits(v string) bool {
	if v == "" {
		return false
	}
	for i := 0; i < len(v); i++ {
		if v[i] < '0' || v[i] > '9' {
			return false
		}
	}
	return true
}

// waitThrottle sleeps for d, aborting promptly when ctx is cancelled.
func waitThrottle(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// jitterFraction is the default jitter source; swapped in tests.
func jitterFraction() float64 { return rand.Float64() }
