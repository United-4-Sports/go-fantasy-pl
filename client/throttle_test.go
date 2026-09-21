package client

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// step is one scripted upstream response.
type step struct {
	status     int
	retryAfter string // Retry-After header value; empty omits the header
}

// scriptedServer serves the given steps in order and repeats the last one.
// hits counts requests.
func scriptedServer(t *testing.T, steps ...step) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idx := int(hits.Add(1)) - 1
		s := steps[len(steps)-1]
		if idx < len(steps) {
			s = steps[idx]
		}
		if s.retryAfter != "" {
			w.Header().Set("Retry-After", s.retryAfter)
		}
		w.WriteHeader(s.status)
		_, _ = io.WriteString(w, "{}")
	}))
	t.Cleanup(server.Close)
	return server, &hits
}

// sleepRecorder replaces the client's throttle wait so tests assert planned
// delays instead of sleeping.
type sleepRecorder struct {
	mu    sync.Mutex
	waits []time.Duration
	err   error // returned for every wait when non-nil
}

func (s *sleepRecorder) wait(ctx context.Context, d time.Duration) error {
	s.mu.Lock()
	s.waits = append(s.waits, d)
	err := s.err
	s.mu.Unlock()
	if err != nil {
		return err
	}
	return ctx.Err()
}

func (s *sleepRecorder) recorded() []time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Duration(nil), s.waits...)
}

// newThrottleTestClient builds a client against the scripted server with
// deterministic throttle plumbing: no real sleeps and jitter pinned to the
// nominal backoff (multiplier 1.0).
func newThrottleTestClient(t *testing.T, server *httptest.Server, opts ...Option) (*Client, *sleepRecorder) {
	t.Helper()
	rec := &sleepRecorder{}
	c, err := NewClient(append([]Option{
		WithMemoryCache(),
		WithBaseURL(server.URL),
		WithRateLimit(1000, time.Minute),
	}, opts...)...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	c.throttleSleep = rec.wait
	c.throttleJitter = func() float64 { return 0.5 }
	return c, rec
}

func TestThrottle429HonorsRetryAfterThenSucceeds(t *testing.T) {
	server, hits := scriptedServer(t, step{status: 429, retryAfter: "2"}, step{status: 200})
	c, rec := newThrottleTestClient(t, server)
	var events []ThrottleEvent
	var mu sync.Mutex
	c.throttleObserver = func(e ThrottleEvent) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	}

	resp, err := c.Get("/bootstrap-static/")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, int32(2), hits.Load(), "429 must be retried exactly once")
	require.Equal(t, []time.Duration{2 * time.Second}, rec.recorded(), "must wait exactly Retry-After")

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, events, 1)
	assert.Equal(t, http.StatusTooManyRequests, events[0].StatusCode)
	assert.Equal(t, 1, events[0].Attempt)
	assert.Equal(t, 2*time.Second, events[0].Wait)
	assert.Equal(t, 2*time.Second, events[0].RetryAfter)
	assert.True(t, events[0].WillRetry)
	assert.Equal(t, "/bootstrap-static/", events[0].Endpoint)
}

func TestThrottleSecond429SurfacesResponse(t *testing.T) {
	// The default budget is a single 429 retry: a second consecutive 429
	// reaches the caller unmodified, with a WillRetry=false event.
	server, hits := scriptedServer(t, step{status: 429, retryAfter: "1"})
	c, rec := newThrottleTestClient(t, server)
	var gaveUp atomic.Bool
	c.throttleObserver = func(e ThrottleEvent) {
		if !e.WillRetry {
			gaveUp.Store(true)
		}
	}

	resp, err := c.Get("/bootstrap-static/")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	assert.Equal(t, "1", resp.Header.Get("Retry-After"), "surfaced response must be unmodified")
	assert.Equal(t, int32(2), hits.Load())
	assert.Equal(t, []time.Duration{1 * time.Second}, rec.recorded())
	assert.True(t, gaveUp.Load(), "exhausted budget must emit a WillRetry=false event")
}

func TestThrottle429WithoutHeaderUsesBaseBackoff(t *testing.T) {
	server, hits := scriptedServer(t, step{status: 429})
	c, rec := newThrottleTestClient(t, server)

	resp, err := c.Get("/bootstrap-static/")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	assert.Equal(t, int32(2), hits.Load())
	// Jitter pinned to nominal: the single 429 retry waits exactly BaseBackoff.
	require.Equal(t, []time.Duration{500 * time.Millisecond}, rec.recorded())
}

func TestThrottle403BackoffThenSurfaces(t *testing.T) {
	// Default policy retries a 403 twice with exponential delays (jitter
	// pinned to nominal), then surfaces the third 403.
	server, hits := scriptedServer(t, step{status: 403})
	c, rec := newThrottleTestClient(t, server)

	resp, err := c.Get("/bootstrap-static/")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	require.Equal(t, int32(3), hits.Load(), "default policy is 2 retries after the first attempt")
	assert.Equal(t, []time.Duration{500 * time.Millisecond, time.Second}, rec.recorded())
}

func TestThrottle403ThenSucceeds(t *testing.T) {
	server, hits := scriptedServer(t, step{status: 403}, step{status: 403}, step{status: 200})
	c, rec := newThrottleTestClient(t, server)

	resp, err := c.Get("/bootstrap-static/")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, int32(3), hits.Load())
	assert.Equal(t, []time.Duration{500 * time.Millisecond, time.Second}, rec.recorded())
}

func TestThrottleMixedStatusBudgetsAreIndependent(t *testing.T) {
	// One 429 retry and two 403 retries are separate budgets: a request
	// throttled in both classes gets all of them.
	server, hits := scriptedServer(t,
		step{status: 429, retryAfter: "0"},
		step{status: 403},
		step{status: 403},
		step{status: 403},
	)
	c, rec := newThrottleTestClient(t, server)

	resp, err := c.Get("/bootstrap-static/")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	require.Equal(t, int32(4), hits.Load())
	assert.Equal(t, []time.Duration{0, 500 * time.Millisecond, time.Second}, rec.recorded())
}

func TestThrottleBackoffJitterBounds(t *testing.T) {
	// With real jitter each 403 wait must land in [0.5x, 1.5x) of nominal.
	server, _ := scriptedServer(t, step{status: 403})
	c, _ := newThrottleTestClient(t, server)
	c.throttleJitter = jitterFraction // real randomness

	var mu sync.Mutex
	var waits []time.Duration
	c.throttleSleep = func(ctx context.Context, d time.Duration) error {
		mu.Lock()
		waits = append(waits, d)
		mu.Unlock()
		return nil
	}

	resp, err := c.Get("/bootstrap-static/")
	require.NoError(t, err)
	defer resp.Body.Close()

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, waits, 2)
	for i, w := range waits {
		nominal := 500 * time.Millisecond << i
		assert.GreaterOrEqual(t, w, nominal/2, "wait %d below jitter floor", i)
		assert.Less(t, w, nominal*3/2, "wait %d above jitter ceiling", i)
	}
}

func TestThrottleCancellationDuringBackoff(t *testing.T) {
	server, hits := scriptedServer(t, step{status: 403})
	c, _ := newThrottleTestClient(t, server)
	c.throttleSleep = func(ctx context.Context, d time.Duration) error {
		return context.Canceled
	}

	resp, err := c.Get("/bootstrap-static/")
	require.Error(t, err)
	assert.Nil(t, resp)
	require.ErrorIs(t, err, context.Canceled)
	assert.Contains(t, err.Error(), "throttle backoff")
	assert.Equal(t, int32(1), hits.Load(), "no further attempts after cancellation")
}

func TestThrottleAlreadyCancelledContext(t *testing.T) {
	server, hits := scriptedServer(t, step{status: 403})
	c, _ := newThrottleTestClient(t, server)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.GetContext(ctx, "/bootstrap-static/")
	require.ErrorIs(t, err, context.Canceled)
	assert.Zero(t, hits.Load(), "cancelled context must not reach HTTP")
}

func TestThrottleDisabledRestoresSingleAttempt(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusForbidden} {
		t.Run(fmt.Sprintf("status_%d", status), func(t *testing.T) {
			server, hits := scriptedServer(t, step{status: status})
			var events atomic.Int32
			c, rec := newThrottleTestClient(t, server, WithThrottlePolicy(ThrottlePolicy{Disabled: true}))
			c.throttleObserver = func(ThrottleEvent) { events.Add(1) }

			resp, err := c.Get("/bootstrap-static/")
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, status, resp.StatusCode)
			assert.Equal(t, int32(1), hits.Load())
			assert.Zero(t, events.Load(), "disabled policy emits no events")
			assert.Empty(t, rec.recorded())
		})
	}
}

func TestThrottleNonRetryableStatusesNeverRetry(t *testing.T) {
	// Pins the compatibility guarantee: every other status reaches callers
	// on the first attempt, so service-level error mapping is unchanged.
	for _, status := range []int{http.StatusNotFound, http.StatusTeapot, http.StatusInternalServerError} {
		t.Run(fmt.Sprintf("status_%d", status), func(t *testing.T) {
			server, hits := scriptedServer(t, step{status: status})
			c, rec := newThrottleTestClient(t, server)

			resp, err := c.Get("/bootstrap-static/")
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, status, resp.StatusCode)
			assert.Equal(t, int32(1), hits.Load())
			assert.Empty(t, rec.recorded())
		})
	}
}

func TestThrottleRetriesConsumeLimiterTokens(t *testing.T) {
	// A one-token limiter admits the first attempt; the 429 retry must
	// queue on the limiter again, proving retries consume tokens. The
	// parked retry is then released by cancelling the request context
	// (the limiter wait returns context.Canceled).
	server, hits := scriptedServer(t, step{status: 429, retryAfter: "0"}, step{status: 200})
	c, _ := newThrottleTestClient(t, server)
	r := fixedRateLimiter(1, time.Hour)
	c.rateLimit = r
	queued := make(chan struct{})
	var once sync.Once
	r.waitHook = func() { once.Do(func() { close(queued) }) }

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		resp, err := c.GetContext(ctx, "/bootstrap-static/")
		if err == nil {
			_ = resp.Body.Close()
		}
		errCh <- err
	}()

	select {
	case <-queued:
		cancel()
		select {
		case err := <-errCh:
			require.Error(t, err)
			require.ErrorIs(t, err, context.Canceled)
			assert.Contains(t, err.Error(), "rate limit wait failed",
				"the failure must come from the retry's limiter wait")
		case <-time.After(5 * time.Second):
			t.Fatal("parked retry never returned after cancellation")
		}
		assert.Equal(t, int32(1), hits.Load(), "the retry never reached HTTP")
	case err := <-errCh:
		t.Fatalf("call finished without ever queueing on the limiter: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("retry never queued on the limiter")
	}
	cancel()
}

// bodyTracker wraps a transport and counts response bodies that have not
// been closed yet.
type bodyTracker struct {
	rt   http.RoundTripper
	mu   sync.Mutex
	open int
}

func (b *bodyTracker) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := b.rt.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	b.open++
	b.mu.Unlock()
	resp.Body = &trackedBody{ReadCloser: resp.Body, t: b}
	return resp, nil
}

func (b *bodyTracker) openBodies() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.open
}

type trackedBody struct {
	io.ReadCloser
	t    *bodyTracker
	once sync.Once
}

func (tb *trackedBody) Close() error {
	tb.once.Do(func() {
		tb.t.mu.Lock()
		tb.t.open--
		tb.t.mu.Unlock()
	})
	return tb.ReadCloser.Close()
}

func TestThrottleBodiesClosedBetweenAttempts(t *testing.T) {
	// Every throttled attempt's body must be closed before the next one;
	// the surfaced final response stays open until the caller closes it.
	server, hits := scriptedServer(t, step{status: 403}, step{status: 403}, step{status: 200})
	c, _ := newThrottleTestClient(t, server)
	tracker := &bodyTracker{rt: c.httpClient.Transport}
	c.httpClient.Transport = tracker

	resp, err := c.Get("/bootstrap-static/")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, int32(3), hits.Load())
	assert.Equal(t, 1, tracker.openBodies(), "only the final response may still be open; throttled bodies close before the next attempt")

	require.NoError(t, resp.Body.Close())
	assert.Zero(t, tracker.openBodies(), "final body belongs to the caller")
}

func TestThrottleRetryAfterCapped(t *testing.T) {
	// A 10-minute Retry-After under the default 2-minute cap waits the
	// cap, not the requested time.
	server, hits := scriptedServer(t, step{status: 429, retryAfter: "600"}, step{status: 200})
	c, rec := newThrottleTestClient(t, server)

	resp, err := c.Get("/bootstrap-static/")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, int32(2), hits.Load())
	require.Equal(t, []time.Duration{2 * time.Minute}, rec.recorded())
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	header := func(v string) http.Header {
		h := http.Header{}
		if v != "" {
			h.Set("Retry-After", v)
		}
		return h
	}

	for _, tc := range []struct {
		name string
		v    string
		want time.Duration
		ok   bool
	}{
		{"absent", "", 0, false},
		{"seconds", "30", 30 * time.Second, true},
		{"zero", "0", 0, true},
		{"negative clamps", "-5", 0, true},
		{"http date future", "Mon, 21 Sep 2026 12:00:02 GMT", 2 * time.Second, true},
		{"http date past", "Mon, 21 Sep 2026 11:59:58 GMT", 0, true},
		{"garbage", "soon", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, ok := parseRetryAfter(header(tc.v), now)
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.want, d)
		})
	}
}

func TestThrottlePolicyResolution(t *testing.T) {
	// Zero value means defaults; -1 disables a class; explicit values pass
	// through; MaxBackoff never resolves below BaseBackoff.
	p := ThrottlePolicy{}.resolved()
	assert.Equal(t, 1, p.Max429Retries)
	assert.Equal(t, 2, p.Max403Retries)
	assert.Equal(t, 500*time.Millisecond, p.BaseBackoff)
	assert.Equal(t, 8*time.Second, p.MaxBackoff)
	assert.Equal(t, 2*time.Minute, p.RetryAfterCap)

	p = ThrottlePolicy{
		Max429Retries: -1,
		Max403Retries: 3,
		BaseBackoff:   time.Second,
		MaxBackoff:    100 * time.Millisecond,
	}.resolved()
	assert.Zero(t, p.Max429Retries, "-1 disables 429 retries")
	assert.Equal(t, 3, p.Max403Retries)
	assert.Equal(t, time.Second, p.BaseBackoff)
	assert.Equal(t, time.Second, p.MaxBackoff, "MaxBackoff clamps up to BaseBackoff")
}

func TestThrottlePolicyOptionLastWins(t *testing.T) {
	c, err := NewClient(WithMemoryCache(),
		WithThrottlePolicy(ThrottlePolicy{Disabled: true}),
		WithThrottlePolicy(ThrottlePolicy{Max403Retries: 5}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	assert.False(t, c.throttle.Disabled)
	assert.Equal(t, 5, c.throttle.Max403Retries)
}

func TestThrottleObserverOptionLastWins(t *testing.T) {
	c, err := NewClient(WithMemoryCache(), WithThrottleObserver(func(ThrottleEvent) {}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	assert.NotNil(t, c.throttleObserver)

	c2, err := NewClient(WithMemoryCache(),
		WithThrottleObserver(func(ThrottleEvent) {}),
		WithThrottleObserver(nil),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c2.Close() })
	assert.Nil(t, c2.throttleObserver, "final nil observer clears the earlier one")
}
