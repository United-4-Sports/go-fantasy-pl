package client

import (
	"fmt"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/AbdoAnss/go-fantasy-pl/endpoints"
	"github.com/AbdoAnss/go-fantasy-pl/internal/cache"
)

// Option is a functional option for configuring the Client.
type Option func(*Client)

// RedisOptions configures the SDK's Redis-backed cache.
type RedisOptions = cache.RedisOptions

// WithHTTPClient sets a custom http.Client for the SDK to use.
func WithHTTPClient(httpClient *http.Client) Option {
	return func(c *Client) {
		c.httpClient = httpClient
	}
}

// WithTimeout sets the timeout for all API requests.
func WithTimeout(timeout time.Duration) Option {
	return func(c *Client) {
		if c.httpClient == nil {
			c.httpClient = &http.Client{}
		}
		c.httpClient.Timeout = timeout
	}
}

// WithBaseURL overrides the default FPL API base URL.
func WithBaseURL(url string) Option {
	return func(c *Client) {
		c.baseURL = url
	}
}

// WithRateLimit configures the client's internal rate limiter.
func WithRateLimit(requests int, interval time.Duration) Option {
	return func(c *Client) {
		c.rateLimit = newRateLimiter(requests, interval)
	}
}

// WithRedisCache configures the client to use a Redis-backed distributed cache.
// This overrides the default cache selection, enabling shared state across
// multiple instances (e.g., in a horizontally-scaled microservice deployment).
// NewClient will return an error if the Redis server is unreachable.
func WithRedisCache(opts RedisOptions) Option {
	return func(c *Client) {
		c.cacheSet = true
		c.cache = nil
		rc, err := cache.NewRedisCache(opts)
		if err != nil {
			c.cacheErr = err
			return
		}
		endpoints.SetSharedCache(rc)
	}
}

// WithCache binds a caller-owned cache to this client without replacing the
// package-global cache. Callers sharing a store share endpoint entries, so use
// separate stores or Redis prefixes for different upstream data sources.
func WithCache(store Cache) Option {
	return func(c *Client) {
		c.cacheSet = true
		if store == nil {
			c.cacheErr = fmt.Errorf("cache must not be nil")
			return
		}
		c.cache = store
	}
}

// WithRedisCacheClient borrows an existing pool without pinging, replacing the
// global cache, or taking ownership. The caller configures its DB and timeouts
// and closes it after all SDK/application users have stopped. Use a dedicated
// key prefix for SDK entries and another prefix for application snapshots.
func WithRedisCacheClient(pool *redis.Client, keyPrefix string) Option {
	return func(c *Client) {
		c.cacheSet = true
		if pool == nil {
			c.cacheErr = fmt.Errorf("redis client must not be nil")
			return
		}
		c.cache = cache.NewRedisCacheWithClient(pool, keyPrefix)
	}
}

// WithMemoryCache forces the SDK to use the in-memory cache backend.
func WithMemoryCache() Option {
	return func(c *Client) {
		c.cacheSet = true
		c.cache = nil
		endpoints.SetSharedCache(defaultMemoryCache)
	}
}
