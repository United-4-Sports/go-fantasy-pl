package client_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/AbdoAnss/go-fantasy-pl/client"
	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"
)

// newCountingFixturesServer returns an upstream whose payload encodes the hit
// count, so tests can tell a fresh fetch from a cache hit.
func newCountingFixturesServer(t *testing.T) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = fmt.Fprintf(w, `[{"id":1,"code":%d}]`, hits.Load())
	}))
	t.Cleanup(server.Close)
	return server, &hits
}

// Two default memory clients share warming: A fetches, B reads with one total
// HTTP hit. Mutation of a returned result must not leak into the shared store.
func TestDefaultMemoryClientsShareWarming(t *testing.T) {
	t.Setenv("FPL_CACHE_BACKEND", "memory")
	server, hits := newCountingFixturesServer(t)
	a, err := client.NewClient(client.WithBaseURL(server.URL))
	require.NoError(t, err)
	b, err := client.NewClient(client.WithBaseURL(server.URL))
	require.NoError(t, err)
	t.Cleanup(func() { a.Cache().Clear(); b.Cache().Clear() })

	first, err := a.Fixtures.GetAllFixtures()
	require.NoError(t, err)
	// Mutating the returned slice must not corrupt another caller's view.
	first[0].Code = 999999

	second, err := b.Fixtures.GetAllFixtures()
	require.NoError(t, err)
	require.Len(t, second, 1)
	require.NotEqual(t, int64(999999), second[0].Code, "caller mutation must not leak into the shared cache")
	require.EqualValues(t, 1, hits.Load(), "second client must be served from the shared store")
}

// Same Redis DB/prefix shares entries even across different pools; different
// DB or prefix isolates different upstream datasets.
func TestRedisClientsShareEntriesByDBAndPrefix(t *testing.T) {
	mr := miniredis.RunT(t)
	server, hits := newCountingFixturesServer(t)

	sameDB := func(prefix string, db int) *client.Client {
		t.Helper()
		c, err := client.NewClient(client.WithBaseURL(server.URL), client.WithRedisCache(client.RedisOptions{Addr: mr.Addr(), DB: db, KeyPrefix: prefix}))
		require.NoError(t, err)
		owned := c.Cache()
		t.Cleanup(func() {
			if closer, ok := owned.(interface{ Close() error }); ok {
				require.NoError(t, closer.Close())
			}
		})
		return c
	}

	a := sameDB("sdk", 3)
	b := sameDB("sdk", 3)
	first, err := a.Fixtures.GetAllFixtures()
	require.NoError(t, err)
	again, err := b.Fixtures.GetAllFixtures()
	require.NoError(t, err)
	require.Equal(t, first, again)
	require.EqualValues(t, 1, hits.Load(), "same DB and prefix must share entries")
	require.True(t, mr.DB(3).Exists("sdk:fixtures"))
	require.False(t, mr.DB(0).Exists("sdk:fixtures"), "entries must land in the configured DB")

	otherPrefix := sameDB("other-upstream", 3)
	other, err := otherPrefix.Fixtures.GetAllFixtures()
	require.NoError(t, err)
	require.Len(t, other, 1)
	require.EqualValues(t, 2, hits.Load(), "different prefix must be isolated from sdk:*")

	otherDB := sameDB("sdk", 4)
	_, err = otherDB.Fixtures.GetAllFixtures()
	require.NoError(t, err)
	require.EqualValues(t, 3, hits.Load(), "different DB must be isolated from DB 3")
}
