package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newBootstrapServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bootstrap-static/" {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"teams":[{"id":1,"name":"Arsenal","short_name":"ARS","code":3,"strength":5}],
			"elements":[{"id":1,"web_name":"Player One","code":101}],
			"events":[{"id":1,"is_current":true}],
			"game_settings":{"league_join_private_max":20}
		}`))
	}))
}

func TestNewClient(t *testing.T) {
	c, err := NewClient(
		WithTimeout(20*time.Second),
		WithRateLimit(30, time.Minute),
	)

	// Use Testify's assert package for better readability
	assert.NoError(t, err, "Expected no error creating client")
	assert.NotNil(t, c, "Expected non-nil client")
	assert.Equal(t, baseURL, c.baseURL, "Expected baseURL to be %s, got %s", baseURL, c.baseURL)
}

func TestNewClient_WithRedisCache(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	c, err := NewClient(
		WithRedisCache(RedisOptions{Addr: mr.Addr(), KeyPrefix: "test"}),
	)
	require.NoError(t, err)
	assert.NotNil(t, c)
}

func TestNewClient_WithRedisCache_UnreachableServer(t *testing.T) {
	_, err := NewClient(
		WithRedisCache(RedisOptions{Addr: "localhost:19999"}),
	)
	assert.Error(t, err, "should fail when Redis server is unreachable")
}

func TestNewClient_DefaultCacheUsesRedisWhenAvailable(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	server := newBootstrapServer()
	defer server.Close()

	t.Setenv("REDIS_ADDR", mr.Addr())
	t.Setenv("REDIS_KEY_PREFIX", "default-cache")
	t.Setenv("REDIS_PASSWORD", "")
	t.Setenv("REDIS_DB", "0")
	t.Setenv("FPL_CACHE_BACKEND", "")

	c, err := NewClient(WithBaseURL(server.URL))
	require.NoError(t, err)

	teams, err := c.Bootstrap.GetTeams()
	require.NoError(t, err)
	require.Len(t, teams, 1)
	assert.True(t, mr.Exists("default-cache:teams"))
}

func TestNewClient_DefaultCacheFallsBackToMemoryWhenRedisUnavailable(t *testing.T) {
	server := newBootstrapServer()
	defer server.Close()

	t.Setenv("REDIS_ADDR", "localhost:19999")
	t.Setenv("REDIS_KEY_PREFIX", "fallback-cache")
	t.Setenv("REDIS_PASSWORD", "")
	t.Setenv("REDIS_DB", "0")
	t.Setenv("FPL_CACHE_BACKEND", "")

	c, err := NewClient(WithBaseURL(server.URL))
	require.NoError(t, err)

	teams, err := c.Bootstrap.GetTeams()
	require.NoError(t, err)
	require.Len(t, teams, 1)
}

func TestNewClient_DefaultCache_StrictRedisModeReturnsError(t *testing.T) {
	t.Setenv("REDIS_ADDR", "localhost:19999")
	t.Setenv("REDIS_PASSWORD", "")
	t.Setenv("REDIS_DB", "0")
	t.Setenv("FPL_CACHE_BACKEND", "redis")

	_, err := NewClient()
	assert.Error(t, err, "should fail when strict Redis mode is enabled and Redis is unreachable")
}

func TestNewClient_WithMemoryCacheOverridesDefaultRedis(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	server := newBootstrapServer()
	defer server.Close()

	t.Setenv("REDIS_ADDR", mr.Addr())
	t.Setenv("REDIS_KEY_PREFIX", "memory-override")
	t.Setenv("REDIS_PASSWORD", "")
	t.Setenv("REDIS_DB", "0")
	t.Setenv("FPL_CACHE_BACKEND", "")

	c, err := NewClient(
		WithBaseURL(server.URL),
		WithMemoryCache(),
	)
	require.NoError(t, err)

	teams, err := c.Bootstrap.GetTeams()
	require.NoError(t, err)
	require.Len(t, teams, 1)
	assert.Empty(t, mr.Keys(), "memory override must not write Redis")
}

func TestGetRaw_ReturnsUndecodedBody(t *testing.T) {
	payload := []byte(`{"has_next": true, "page": 1, "results": []}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/leagues-h2h-matches/league/1/", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	c, err := NewClient(WithBaseURL(server.URL), WithMemoryCache())
	require.NoError(t, err)

	body, err := c.GetRaw("/leagues-h2h-matches/league/1/")
	require.NoError(t, err)
	assert.Equal(t, payload, body)
}

func TestGetRaw_NonOKStatusFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	c, err := NewClient(WithBaseURL(server.URL), WithMemoryCache())
	require.NoError(t, err)

	body, err := c.GetRaw("/entry/1/")
	assert.Nil(t, body)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected status code")
}

func TestGetRaw_NetworkErrorFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	addr := server.URL
	server.Close() // close immediately so the connection is refused

	c, err := NewClient(WithBaseURL(addr), WithMemoryCache())
	require.NoError(t, err)

	body, err := c.GetRaw("/bootstrap-static/")
	assert.Nil(t, body)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "request failed")
}

func TestRateLimitOptionValidationAndOrdering(t *testing.T) {
	for _, tc := range []struct {
		name      string
		opts      []Option
		wantError string
	}{
		{"zero requests", []Option{WithRateLimit(0, time.Second)}, "rate limit configuration"},
		{"negative requests", []Option{WithRateLimit(-1, time.Second)}, "rate limit configuration"},
		{"zero interval", []Option{WithRateLimit(1, 0)}, "rate limit configuration"},
		{"negative interval", []Option{WithRateLimit(1, -time.Second)}, "rate limit configuration"},
		{"invalid then valid", []Option{WithRateLimit(0, 0), WithRateLimit(3, time.Second)}, ""},
		{"valid then invalid", []Option{WithRateLimit(3, time.Second), WithRateLimit(0, 0)}, "rate limit configuration"},
		{"valid then valid", []Option{WithRateLimit(9, time.Hour), WithRateLimit(3, time.Second)}, ""},
		{"cache error survives rate correction", []Option{WithCache(nil), WithRateLimit(0, 0), WithRateLimit(3, time.Second)}, "cache configuration"},
		{"cache correction preserves rate error", []Option{WithRateLimit(0, 0), WithCache(nil), WithMemoryCache()}, "rate limit configuration"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := append([]Option{WithMemoryCache()}, tc.opts...)
			c, err := NewClient(opts...)
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				assert.Nil(t, c)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, 3, c.rateLimit.maxTokens)
			assert.Equal(t, time.Second, c.rateLimit.interval)
		})
	}
}

func TestGetContextRateLimitCancellation(t *testing.T) {
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	c, err := NewClient(WithMemoryCache(), WithBaseURL(server.URL), WithRateLimit(1, time.Hour))
	require.NoError(t, err)
	r := fixedRateLimiter(1, time.Hour)
	c.rateLimit = r
	now := r.lastRefill
	r.clock = func() time.Time { return now }

	// Already-cancelled contexts must not consume the initial burst or hit HTTP.
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	resp, err := c.GetContext(cancelled, "/")
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, resp)
	assert.Equal(t, 1, r.tokens)
	assert.Zero(t, hits.Load())

	resp, err = c.Get("/")
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, int64(1), hits.Load())
	require.Zero(t, r.tokens, "Get must use the same limiter as GetContext")

	queued := make(chan struct{})
	var once sync.Once
	r.waitHook = func() { once.Do(func() { close(queued) }) }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		resp, err := c.GetContext(ctx, "/")
		if resp != nil {
			_ = resp.Body.Close()
		}
		done <- err
	}()
	select {
	case <-queued:
	case <-time.After(10 * time.Second):
		t.Fatal("request never reached limiter queue")
	}
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(10 * time.Second):
		t.Fatal("queued request ignored cancellation")
	}
	require.Equal(t, int64(1), hits.Load(), "cancelled waiter must not reach upstream")
	assert.Zero(t, r.tokens)

	// The next earned token is still available to another caller.
	now = now.Add(time.Hour)
	outer, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	resp, err = c.GetContext(outer, "/")
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, int64(2), hits.Load())
	assert.Zero(t, r.tokens)
}

func TestGetContextExpiredContext(t *testing.T) {
	c, err := NewClient(WithMemoryCache())
	require.NoError(t, err)
	ctx, cancel := context.WithDeadline(context.Background(), time.Unix(0, 0))
	defer cancel()
	resp, err := c.GetContext(ctx, "/")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Nil(t, resp)
	assert.Equal(t, 50, c.rateLimit.tokens)
}
