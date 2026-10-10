package endpoints_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AbdoAnss/go-fantasy-pl/client"
	"github.com/AbdoAnss/go-fantasy-pl/internal/cache"
	"github.com/AbdoAnss/go-fantasy-pl/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// partialClassicLeagueCache models a cache implementation that can partially
// populate its destination before reporting a decode failure.
type partialClassicLeagueCache struct {
	*cache.MemoryCache

	mu     sync.Mutex
	writes map[string][]byte
}

func newPartialClassicLeagueCache() *partialClassicLeagueCache {
	return &partialClassicLeagueCache{
		MemoryCache: cache.NewMemoryCache(),
		writes:      make(map[string][]byte),
	}
}

func (c *partialClassicLeagueCache) GetContext(ctx context.Context, key string, dest any) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if key == "classic_league_42_page_1" {
		// Keep the valid prefix so the fallback network decode has a stale
		// field to accidentally preserve if the destination is shared.
		if err := json.Unmarshal([]byte(`{"league":{"name":"stale-cache-name","id":"not-an-int"}}`), dest); err != nil {
			return false, err
		}
		return false, errors.New("injected cache decode error")
	}
	return c.MemoryCache.GetContext(ctx, key, dest)
}

func (c *partialClassicLeagueCache) SetContext(ctx context.Context, key string, value any, ttl time.Duration) error {
	if err := c.MemoryCache.SetContext(ctx, key, value, ttl); err != nil {
		return err
	}
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.writes[key] = append([]byte(nil), body...)
	c.mu.Unlock()
	return nil
}

func TestClassicLeagueCacheDecodeErrorDoesNotContaminateNetworkResult(t *testing.T) {
	store := newPartialClassicLeagueCache()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		assert.Equal(t, "/leagues-classic/42/standings/", r.URL.Path)
		assert.Equal(t, "1", r.URL.Query().Get("page_standings"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"league":{"id":42},"standings":{"results":[]}}`))
	}))
	defer server.Close()

	var diagnostics []struct {
		operation string
		err       error
	}
	c, err := client.NewClient(
		client.WithBaseURL(server.URL),
		client.WithCache(store),
		client.WithCacheErrorHandler(func(operation string, err error) {
			diagnostics = append(diagnostics, struct {
				operation string
				err       error
			}{operation: operation, err: err})
		}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	league, err := c.Leagues.GetClassicLeagueStandings(42, 1)
	require.NoError(t, err)
	require.NotNil(t, league)
	require.Equal(t, 42, league.League.ID)
	require.Empty(t, league.League.Name, "partial cache data must not survive a fresh network decode")
	require.Equal(t, int32(1), requests.Load())

	require.Len(t, diagnostics, 1)
	require.Equal(t, "get", diagnostics[0].operation)
	require.Error(t, diagnostics[0].err)

	const cacheKey = "classic_league_42_page_1"
	store.mu.Lock()
	body, ok := store.writes[cacheKey]
	store.mu.Unlock()
	require.True(t, ok, "the fresh response should be written back to the cache")
	require.NotContains(t, string(body), "stale-cache-name")

	var cached models.ClassicLeague
	require.NoError(t, json.Unmarshal(body, &cached))
	require.Equal(t, 42, cached.League.ID)
	require.Empty(t, cached.League.Name)
}
