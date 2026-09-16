package client_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AbdoAnss/go-fantasy-pl/client"
	"github.com/AbdoAnss/go-fantasy-pl/endpoints"
	"github.com/AbdoAnss/go-fantasy-pl/internal/cache"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestBorrowedRedisPool(t *testing.T) {
	mr := miniredis.RunT(t)
	pool := redis.NewClient(&redis.Options{Addr: mr.Addr(), DB: 1, PoolSize: 1, MaxActiveConns: 1})
	t.Cleanup(func() { require.NoError(t, pool.Close()) })
	var hits atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if _, err := w.Write([]byte(`[{"id":1,"code":101}]`)); err != nil {
			t.Errorf("write upstream response: %v", err)
		}
	}))
	defer upstream.Close()
	old := endpoints.GetSharedCache()
	t.Cleanup(func() { endpoints.SetSharedCache(old) })
	c, err := client.NewClient(client.WithBaseURL(upstream.URL), client.WithRedisCacheClient(pool, "sdk"))
	require.NoError(t, err)
	require.Same(t, old, endpoints.GetSharedCache(), "borrowed option replaced global cache")
	first, err := c.Fixtures.GetAllFixtures()
	require.NoError(t, err)
	key := "sdk:fixtures"
	require.True(t, mr.DB(1).Exists(key))
	require.False(t, mr.DB(0).Exists(key))
	ctx := context.Background()
	require.NoError(t, pool.Set(ctx, "app:precompute:v1:captain", `[]`, time.Minute).Err())
	// Reconfiguring legacy clients cannot redirect this explicit borrowed cache.
	_, err = client.NewClient(client.WithMemoryCache())
	require.NoError(t, err)
	second, err := client.NewClient(client.WithBaseURL(upstream.URL), client.WithRedisCacheClient(pool, "sdk"))
	require.NoError(t, err)
	again, err := second.Fixtures.GetAllFixtures()
	require.NoError(t, err)
	require.Equal(t, first, again)
	require.EqualValues(t, 1, hits.Load())
	require.EqualValues(t, 1, pool.PoolStats().TotalConns, "application and SDK should reuse one pool")
	require.NoError(t, pool.Ping(ctx).Err(), "SDK must leave caller-owned pool open")
}

func TestExplicitCacheValidation(t *testing.T) {
	_, err := client.NewClient(client.WithCache(nil))
	require.Error(t, err)
	_, err = client.NewClient(client.WithRedisCacheClient(nil, "sdk"))
	require.Error(t, err)
	store := cache.NewMemoryCache()
	c, err := client.NewClient(client.WithCache(store))
	require.NoError(t, err)
	require.Same(t, store, c.Cache())
}
