package client

import (
	"errors"
	"testing"

	"github.com/AbdoAnss/go-fantasy-pl/endpoints"
	"github.com/AbdoAnss/go-fantasy-pl/internal/cache"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestCacheOptionOrdering(t *testing.T) {
	mr := miniredis.RunT(t)
	pool := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { require.NoError(t, pool.Close()) })
	old := endpoints.GetSharedCache()
	t.Cleanup(func() { endpoints.SetSharedCache(old) })
	store := cache.NewMemoryCache()
	valid := []struct {
		name   string
		option Option
	}{
		{"memory", WithMemoryCache()},
		{"explicit", WithCache(store)},
		{"borrowed Redis", WithRedisCacheClient(pool, "test")},
		{"owned Redis", WithRedisCache(RedisOptions{Addr: mr.Addr(), KeyPrefix: "test"})},
	}
	invalid := []struct {
		name   string
		option Option
	}{
		{"nil cache", WithCache(nil)},
		{"nil Redis client", WithRedisCacheClient(nil, "test")},
		// Simulate the state a failed Redis setup leaves, without a network timeout.
		{"prior Redis setup error", func(c *Client) {
			c.cacheSet = true
			c.cacheErr = errors.New("redis setup failed")
		}},
	}
	for _, good := range valid {
		for _, bad := range invalid {
			t.Run(good.name+" after "+bad.name, func(t *testing.T) {
				// WithRedisCache creates a pool; close it even when assertions fail.
				if good.name == "owned Redis" {
					t.Cleanup(func() {
						if owned, ok := endpoints.GetSharedCache().(*cache.RedisCache); ok {
							require.NoError(t, owned.Close())
						}
						endpoints.SetSharedCache(old)
					})
				}
				c, err := NewClient(bad.option, good.option)
				require.NoError(t, err)
				require.NotNil(t, c)
				switch good.name {
				case "memory", "owned Redis":
					require.Nil(t, c.Cache())
				case "explicit":
					require.Same(t, store, c.Cache())
				case "borrowed Redis":
					require.NotNil(t, c.Cache())
				}
			})
			t.Run(bad.name+" after "+good.name, func(t *testing.T) {
				if good.name == "owned Redis" {
					t.Cleanup(func() {
						if owned, ok := endpoints.GetSharedCache().(*cache.RedisCache); ok {
							require.NoError(t, owned.Close())
						}
						endpoints.SetSharedCache(old)
					})
				}
				c, err := NewClient(good.option, bad.option)
				require.Error(t, err)
				require.Nil(t, c)
			})
		}
	}
}
