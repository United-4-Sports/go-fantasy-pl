// Package client provides the main entry point for the Fantasy Premier League SDK.
// It includes the Client struct which handles authentication, rate limiting, and
// initializes all domain-specific services.
package client

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/AbdoAnss/go-fantasy-pl/endpoints"
	"github.com/AbdoAnss/go-fantasy-pl/internal/cache"
)

const (
	// baseURL is the primary entry point for the official FPL API.
	baseURL = "https://fantasy.premierleague.com/api"
	// defaultTimeout is the default HTTP timeout used if none is specified.
	defaultTimeout = 10 * time.Second
)

// Client is the main SDK client used to interact with the FPL API.
// It coordinates services, manages rate limiting, and handles HTTP communication.
type Client struct {
	cacheErrorHandler func(operation string, err error)
	httpClient        *http.Client
	baseURL           string
	rateLimit         *rateLimiter
	rateLimitErr      error // final rate option's configuration error
	cacheErr          error // stores errors from cache configuration to be returned by NewClient
	cacheSet          bool
	cache             cache.Cache   // retained selection; shared storage is independent of selection
	redisOptions      *RedisOptions // deferred owned pool construction
	throttle          ThrottlePolicy
	throttleObserver  func(ThrottleEvent)                        // nil unless WithThrottleObserver
	throttleSleep     func(context.Context, time.Duration) error // test seam; real waits are cancellable
	throttleJitter    func() float64                             // test seam for backoff jitter

	// Ownership: Close releases only these. Caller-supplied pools, caches and
	// transports are never closed, and shared stores are never cleared.
	ownedCache        bool // SDK created the cache and its pool (WithRedisCache, env Redis)
	ownsHTTPTransport bool // SDK created the default transport (no WithHTTPClient)
	closeOnce         sync.Once
	closeErr          error

	// Bootstrap provides access to core FPL data like players, teams, and gameweeks.
	Bootstrap *endpoints.BootstrapService

	// Players provides methods for fetching player-specific data and history.
	Players *endpoints.PlayerService
	// Fixtures provides methods for fetching match fixtures and results.
	Fixtures *endpoints.FixtureService
	// Teams provides methods for fetching information about Premier League teams.
	Teams *endpoints.TeamService
	// Managers provides methods for fetching manager-specific data and history.
	Managers *endpoints.ManagerService
	// Leagues provides methods for fetching league standings and details.
	Leagues *endpoints.LeagueService
	// Live provides access to gameweek live points data.
	Live *endpoints.LiveService
}

// NewClient creates and returns a new FPL API client.
// It accepts functional options to configure the client's behavior,
// such as custom HTTP clients, timeouts, and caching strategies.
func NewClient(opts ...Option) (*Client, error) {
	c := &Client{
		httpClient: &http.Client{
			Timeout: defaultTimeout,
			Transport: &http.Transport{
				MaxIdleConns:          10,
				IdleConnTimeout:       30 * time.Second,
				DisableCompression:    false,
				DisableKeepAlives:     false,
				MaxConnsPerHost:       10,
				ResponseHeaderTimeout: 10 * time.Second,
			},
		},
		baseURL:   baseURL,
		rateLimit: newRateLimiter(50, time.Minute),
		throttle:  DefaultThrottlePolicy(),
		throttleSleep: func(ctx context.Context, d time.Duration) error {
			return waitThrottle(ctx, d)
		},
		throttleJitter: jitterFraction,
		// The transport above belongs to this client; WithHTTPClient clears this.
		ownsHTTPTransport: true,
	}

	for _, opt := range opts {
		opt(c)
	}

	if c.cacheErr != nil {
		return nil, fmt.Errorf("client: cache configuration failed: %w", c.cacheErr)
	}

	if c.rateLimitErr != nil {
		return nil, fmt.Errorf("client: rate limit configuration failed: %w", c.rateLimitErr)
	}

	var err error
	if !c.cacheSet {
		c.cache, c.ownedCache, err = configureDefaultCache()
	} else if c.redisOptions != nil {
		c.cache, err = cache.NewRedisCache(*c.redisOptions)
		c.ownedCache = true
	}
	if err != nil {
		return nil, fmt.Errorf("client: cache configuration failed: %w", err)
	}

	// Bootstrap service
	c.Bootstrap = endpoints.NewBootstrapService(c)

	// Initialize domain-specific services
	c.Players = endpoints.NewPlayerService(c, c.Bootstrap)
	c.Teams = endpoints.NewTeamService(c, c.Bootstrap)
	c.Managers = endpoints.NewManagerService(c, c.Bootstrap)
	c.Fixtures = endpoints.NewFixtureService(c)
	c.Leagues = endpoints.NewLeagueService(c)
	c.Live = endpoints.NewLiveService(c)

	return c, nil
}

// Cache is the concurrent JSON cache contract accepted by WithCache.
// It is an alias, so existing implementations need no changes.
type Cache = cache.Cache

// Cache returns the cache store selected at construction. SDK clients always
// retain their selection; the legacy shared cache is independent of it.
// The caller owns explicitly supplied cache resources (including pools
// supplied via WithRedisCacheClient).
func (c *Client) Cache() Cache { return c.cache }

// Close releases the resources this client created and is idempotent: repeated
// calls return the same result. It closes an SDK-created Redis pool and closes
// idle connections on the SDK-created HTTP transport.
//
// It deliberately does not: close a caller-supplied Redis pool or cache
// (WithCache, WithRedisCacheClient), close a caller-supplied http.Client or its
// transport (WithHTTPClient), or clear/stop the shared in-memory store, whose
// cleanup task lives for the process so other clients keep working.
//
// Close does not cancel in-flight work: stop requests (cancel their contexts)
// before closing. After Close the client must not be reused when it owned the
// cache, because the closed pool no longer serves reads or writes.
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		if c.ownedCache {
			if closer, ok := c.cache.(interface{ Close() error }); ok {
				c.closeErr = closer.Close()
			}
		}
		// ownsHTTPTransport is only true when this client built the default
		// transport, which is always a non-nil *http.Transport.
		if c.ownsHTTPTransport {
			if transport, ok := c.httpClient.Transport.(*http.Transport); ok {
				transport.CloseIdleConnections()
			}
		}
	})
	return c.closeErr
}

// BaseURL returns the configured base URL for the FPL API.
func (c *Client) BaseURL() string {
	return c.baseURL
}

// Get performs a rate-limited GET request to the specified endpoint relative to the baseURL.
func (c *Client) Get(endpoint string) (*http.Response, error) {
	return c.GetContext(context.Background(), endpoint)
}

// StatusError reports a non-200 response from GetRaw. It is a typed error so
// callers (e.g. the live conformance harness) can branch on the status code
// with errors.As instead of parsing message strings.
type StatusError struct {
	Endpoint   string
	StatusCode int
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("unexpected status code fetching %s: %d", e.Endpoint, e.StatusCode)
}

// GetRaw performs a rate-limited GET request and returns the undecoded
// response body. It is used by the live conformance harness to validate
// models against the exact payload the API returned.
func (c *Client) GetRaw(endpoint string) ([]byte, error) {
	return c.GetRawContext(context.Background(), endpoint)
}

// GetRawContext returns the undecoded response body with context bounding both
// the rate limiter wait and HTTP request. A nil context uses Background.
func (c *Client) GetRawContext(ctx context.Context, endpoint string) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	resp, err := c.GetContext(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, &StatusError{Endpoint: endpoint, StatusCode: resp.StatusCode}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	return body, nil
}

// GetContext performs a rate-limited GET request with a context to the specified endpoint.
// The context bounds both the limiter wait and HTTP request; WithTimeout only
// bounds the HTTP request. Throttled responses (429, throttle-suspected 403)
// are retried with backoff per the client's ThrottlePolicy — see
// WithThrottlePolicy. A nil context uses Background.
func (c *Client) GetContext(ctx context.Context, endpoint string) (*http.Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	policy := c.throttle.resolved()
	budget := map[int]int{
		http.StatusTooManyRequests: policy.Max429Retries,
		http.StatusForbidden:       policy.Max403Retries,
	}
	attempts := map[int]int{}
	attempt := 0

	for {
		if err := c.rateLimit.Wait(ctx); err != nil {
			return nil, fmt.Errorf("rate limit wait failed: %w", err)
		}
		url := c.baseURL + endpoint
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("request failed: %w", err)
		}
		attempt++

		if policy.Disabled || !throttleable(resp.StatusCode) {
			return resp, nil
		}

		// Non-retryable for this status class: surface the response exactly
		// as callers saw it before throttle handling existed.
		retriesLeft := budget[resp.StatusCode] - attempts[resp.StatusCode]
		if retriesLeft <= 0 {
			c.emitThrottle(ThrottleEvent{
				Endpoint: endpoint, StatusCode: resp.StatusCode, Attempt: attempt,
				RetryAfter: retryAfterOf(resp), WillRetry: false,
			})
			return resp, nil
		}

		attempts[resp.StatusCode]++
		wait, retryAfter := c.throttleWait(resp, attempts[resp.StatusCode], policy)
		c.emitThrottle(ThrottleEvent{
			Endpoint: endpoint, StatusCode: resp.StatusCode, Attempt: attempt,
			Wait: wait, RetryAfter: retryAfter, WillRetry: true,
		})

		// The attempt is over: drain a bounded prefix so the connection can
		// be reused, then close the body before waiting.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 8192))
		_ = resp.Body.Close()

		if err := c.throttleSleep(ctx, wait); err != nil {
			return nil, fmt.Errorf("throttle backoff on %s: %w", endpoint, err)
		}
	}
}

// throttleWait computes the delay before the retryN-th retry of this
// status class: a server-provided Retry-After (capped) for 429s, otherwise
// jittered exponential backoff.
func (c *Client) throttleWait(resp *http.Response, retryN int, policy ThrottlePolicy) (wait, retryAfter time.Duration) {
	if resp.StatusCode == http.StatusTooManyRequests {
		if ra, ok := parseRetryAfter(resp.Header, time.Now()); ok {
			wait = min(ra, policy.RetryAfterCap)
			return wait, ra
		}
	}
	return backoff(policy.BaseBackoff, policy.MaxBackoff, retryN, c.throttleJitter), 0
}

// retryAfterOf parses Retry-After for event reporting; absent or invalid
// headers report as zero.
func retryAfterOf(resp *http.Response) time.Duration {
	ra, _ := parseRetryAfter(resp.Header, time.Now())
	return ra
}

func (c *Client) emitThrottle(e ThrottleEvent) {
	if c.throttleObserver != nil {
		c.throttleObserver(e)
	}
}
