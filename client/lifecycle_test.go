package client_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AbdoAnss/go-fantasy-pl/client"
	"github.com/AbdoAnss/go-fantasy-pl/internal/cache"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// spyTransport records CloseIdleConnections so tests can prove the SDK never
// closes a transport it did not create.
type spyTransport struct {
	closes atomic.Int64
}

func (s *spyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return http.DefaultTransport.RoundTrip(r)
}

func (s *spyTransport) CloseIdleConnections() { s.closes.Add(1) }

// The SDK dialed this pool, so Close closes it, and repeated Close is a no-op.
func TestCloseClosesOwnedRedisPoolIdempotently(t *testing.T) {
	mr := miniredis.RunT(t)
	server, _ := newCountingFixturesServer(t)
	c, err := client.NewClient(client.WithBaseURL(server.URL), client.WithRedisCache(client.RedisOptions{Addr: mr.Addr(), KeyPrefix: "t4"}))
	require.NoError(t, err)

	// Prove the pool works before Close.
	_, err = c.Fixtures.GetAllFixtures()
	require.NoError(t, err)

	require.NoError(t, c.Close())
	require.NoError(t, c.Close(), "Close must be idempotent")

	contextual, ok := c.Cache().(cache.ContextCache)
	require.True(t, ok)
	var dest any
	_, err = contextual.GetContext(context.Background(), "t4:fixtures", &dest)
	require.Error(t, err, "owned pool must be closed after Close")
}

// Closing one memory-backed client must not clear the shared store other
// clients still read from.
func TestClosingOneClientKeepsSharedMemoryEntries(t *testing.T) {
	server, hits := newCountingFixturesServer(t)
	a, err := client.NewClient(client.WithBaseURL(server.URL), client.WithMemoryCache())
	require.NoError(t, err)
	a.Cache().Clear()
	t.Cleanup(func() { a.Cache().Clear() })
	b, err := client.NewClient(client.WithBaseURL(server.URL), client.WithMemoryCache())
	require.NoError(t, err)

	first, err := a.Fixtures.GetAllFixtures()
	require.NoError(t, err)
	require.NoError(t, a.Close())

	second, err := b.Fixtures.GetAllFixtures()
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.EqualValues(t, 1, hits.Load(), "closing a client must not clear shared entries")
}

// A timeout configured by the SDK must not rewrite the caller's http.Client,
// and the borrowed transport must survive Close.
func TestWithTimeoutDoesNotMutateCallerHTTPClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // return when the SDK request times out
	}))
	defer server.Close()
	spy := &spyTransport{}
	caller := &http.Client{Timeout: 7 * time.Second, Transport: spy}
	c, err := client.NewClient(
		client.WithBaseURL(server.URL),
		client.WithHTTPClient(caller),
		client.WithTimeout(50*time.Millisecond),
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, c.Close()) })

	require.Equal(t, 7*time.Second, caller.Timeout, "caller-supplied client must not be mutated")
	start := time.Now()
	_, err = c.GetRaw("/bootstrap-static/")
	require.Error(t, err)
	require.Less(t, time.Since(start), 2*time.Second, "SDK must apply its own timeout, not the caller's")

	require.NoError(t, c.Close())
	require.Zero(t, spy.closes.Load(), "SDK must not close a caller-supplied transport")
}

// Two clients borrowing one pool must not affect each other: closing either
// SDK client leaves the shared pool and its entries usable for the other.
func TestClosingOneBorrowerLeavesSharedPoolUsable(t *testing.T) {
	mr := miniredis.RunT(t)
	pool := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { require.NoError(t, pool.Close()) })
	server, _ := newCountingFixturesServer(t)

	a, err := client.NewClient(client.WithBaseURL(server.URL), client.WithRedisCacheClient(pool, "sdk"))
	require.NoError(t, err)
	b, err := client.NewClient(client.WithBaseURL(server.URL), client.WithRedisCacheClient(pool, "sdk"))
	require.NoError(t, err)

	first, err := a.Fixtures.GetAllFixtures()
	require.NoError(t, err)
	require.NoError(t, a.Close())

	second, err := b.Fixtures.GetAllFixtures()
	require.NoError(t, err, "the surviving borrower must keep working")
	require.Equal(t, first, second)
	require.True(t, mr.Exists("sdk:fixtures"), "SDK must not flush borrowed entries")
	require.NoError(t, pool.Ping(context.Background()).Err(), "caller-owned pool must stay open")
	require.NoError(t, b.Close())
}

// A superseded owned-Redis option must never be dialed: an unreachable address
// would fail construction if the SDK connected for an option that lost.
func TestSupersededOwnedRedisOptionDoesNotDial(t *testing.T) {
	c, err := client.NewClient(
		client.WithRedisCache(client.RedisOptions{Addr: "127.0.0.1:1", KeyPrefix: "t4"}),
		client.WithMemoryCache(),
	)
	require.NoError(t, err, "superseded Redis option must not open a connection")
	require.IsType(t, &cache.MemoryCache{}, c.Cache())
	require.NoError(t, c.Close())
}
