package client

import (
	"net/http"
	"net/http/httptest"
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
