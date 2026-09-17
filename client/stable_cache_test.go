package client

import (
	"testing"

	"github.com/AbdoAnss/go-fantasy-pl/endpoints"
	"github.com/AbdoAnss/go-fantasy-pl/internal/cache"
	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"
)

func TestMemoryClientRetainsSelectedCache(t *testing.T) {
	old := endpoints.GetSharedCache()
	t.Cleanup(func() { endpoints.SetSharedCache(old) })
	c, err := NewClient(WithMemoryCache())
	require.NoError(t, err)
	require.Same(t, defaultMemoryCache, c.Cache(), "construction must retain the shared memory store")
	endpoints.SetSharedCache(cache.NewMemoryCache())
	require.Same(t, defaultMemoryCache, c.Cache(), "legacy replacement must not redirect an SDK client")
}

// Constructing a memory client after a Redis client must leave the Redis
// client's selection untouched: selection is per client, storage is shared.
func TestRedisClientRetainsSelectedCacheAcrossMemoryConstruction(t *testing.T) {
	mr := miniredis.RunT(t)
	a, err := NewClient(WithRedisCache(RedisOptions{Addr: mr.Addr(), KeyPrefix: "t2"}))
	require.NoError(t, err)
	owned := a.Cache().(*cache.RedisCache)
	t.Cleanup(func() { require.NoError(t, owned.Close()) })

	b, err := NewClient(WithMemoryCache())
	require.NoError(t, err)
	require.Same(t, defaultMemoryCache, b.Cache())
	require.IsType(t, &cache.RedisCache{}, a.Cache())
}
