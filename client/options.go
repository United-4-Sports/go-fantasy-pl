package client

import (
	"fmt"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/AbdoAnss/go-fantasy-pl/internal/cache"
)

// Option is a functional option for configuring the Client.
type Option func(*Client)

// RedisOptions configures the SDK's Redis-backed cache.
type RedisOptions = cache.RedisOptions

// WithHTTPClient sets a custom http.Client for the SDK to use. The client and
// its transport remain caller-owned: the SDK never closes them, and Close only
// closes idle connections on a transport the SDK created itself.
func WithHTTPClient(httpClient *http.Client) Option {
	return func(c *Client) {
		c.httpClient = httpClient
		c.ownsHTTPTransport = false
	}
}

// WithTimeout sets the timeout for all API requests. A caller-supplied
// http.Client is copied rather than mutated, so a client shared with the
// application keeps its own timeout; the transport itself is still borrowed,
// preserving connection pooling. The final timeout option wins.
func WithTimeout(timeout time.Duration) Option {
	return func(c *Client) {
		if c.httpClient == nil {
			c.httpClient = &http.Client{}
			c.ownsHTTPTransport = true
		}
		if !c.ownsHTTPTransport {
			clone := *c.httpClient
			c.httpClient = &clone
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

// WithRateLimit configures the client's internal per-client rate limiter as a
// token bucket: an initial burst of `requests` tokens, refilled at `requests`
// per `interval` (capacity N per interval ⇒ N/interval tokens per second, so
// `WithRateLimit(50, time.Minute)` sustains 50 requests per minute). The final
// rate option wins, including clearing an earlier rate-option error; unrelated
// cache configuration errors are never cleared. requests and interval must be
// positive; violations are stored and reported by NewClient.
func WithRateLimit(requests int, interval time.Duration) Option {
	return func(c *Client) {
		if requests <= 0 || interval <= 0 {
			c.rateLimitErr = fmt.Errorf("rate limit requires requests > 0 and interval > 0, got requests=%d interval=%s", requests, interval)
			return
		}
		c.rateLimitErr = nil
		c.rateLimit = newRateLimiter(requests, interval)
	}
}

// WithRedisCache configures the client to use its own Redis-backed distributed
// cache, selected at construction and retained for this client's lifetime.
// Entries are shared across SDK clients using the same Redis DB and key
// prefix, even when their pools differ. Other clients and the legacy shared
// cache are unaffected. NewClient will return an error if the Redis server is
// unreachable. The pool is opened only if this is the client's final cache
// option; superseded options never open connections.
func WithRedisCache(opts RedisOptions) Option {
	return func(c *Client) {
		c.cacheErr = nil
		c.cacheSet = true
		c.cache = nil
		// Open only the final selected pool, after all options are validated.
		c.redisOptions = &opts
		c.ownedCache = true
	}
}

// WithCache binds a caller-owned cache to this client, retained for the
// client's lifetime. Callers sharing a store share endpoint entries, so use
// separate stores or Redis prefixes for different upstream data sources. The
// legacy shared cache is unaffected.
func WithCache(store Cache) Option {
	return func(c *Client) {
		c.redisOptions = nil
		c.ownedCache = false
		c.cacheErr = nil
		c.cacheSet = true
		if store == nil {
			c.cacheErr = fmt.Errorf("cache must not be nil")
			return
		}
		c.cache = store
	}
}

// WithRedisCacheClient borrows an existing pool without pinging or taking
// ownership. The caller configures its DB and timeouts and closes it after all
// SDK/application users have stopped. Use a dedicated key prefix for SDK
// entries and another prefix for application snapshots. The selection is
// retained for the client's lifetime; the legacy shared cache is unaffected.
func WithRedisCacheClient(pool *redis.Client, keyPrefix string) Option {
	return func(c *Client) {
		c.redisOptions = nil
		c.ownedCache = false
		c.cacheErr = nil
		c.cacheSet = true
		if pool == nil {
			c.cacheErr = fmt.Errorf("redis client must not be nil")
			return
		}
		c.cache = cache.NewRedisCacheWithClient(pool, keyPrefix)
	}
}

// WithMemoryCache forces the SDK client to use the shared in-memory cache
// backend and retains that selection for the client's lifetime. Entries are
// shared across memory-backed SDK clients; the legacy shared cache is
// unaffected.
func WithMemoryCache() Option {
	return func(c *Client) {
		c.redisOptions = nil
		c.ownedCache = false
		c.cacheErr = nil
		c.cacheSet = true
		c.cache = defaultMemoryCache
	}
}
