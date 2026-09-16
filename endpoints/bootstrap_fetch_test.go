package endpoints_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/AbdoAnss/go-fantasy-pl/client"
	"github.com/AbdoAnss/go-fantasy-pl/endpoints"
	"github.com/AbdoAnss/go-fantasy-pl/internal/cache"
	"github.com/stretchr/testify/require"
)

// TestBootstrapFetchedOnceForAllSections guards the single-fetch contract:
// however many bootstrap sections a caller asks for (even concurrently), a
// cold cache triggers exactly one /bootstrap-static/ download and every
// section is served from it.
func TestBootstrapFetchedOnceForAllSections(t *testing.T) {
	memCache := cache.NewMemoryCache()
	endpoints.SetSharedCache(memCache)

	var bootstrapFetches atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/bootstrap-static/" {
			bootstrapFetches.Add(1)
			writeTestdata(t, w, "bootstrap-static.json")
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	c, err := client.NewClient(
		client.WithBaseURL(server.URL),
		freshMemoryCache(t),
	)
	require.NoError(t, err)

	teams, err := c.Teams.GetAllTeams()
	require.NoError(t, err)
	require.NotEmpty(t, teams)

	players, err := c.Players.GetAllPlayers()
	require.NoError(t, err)
	require.NotEmpty(t, players)

	gameweeks, err := c.Bootstrap.GetGameWeeks()
	require.NoError(t, err)
	require.NotEmpty(t, gameweeks)

	settings, err := c.Bootstrap.GetSettings()
	require.NoError(t, err)
	require.NotZero(t, settings)

	nextGW, err := c.Bootstrap.GetNextGameWeek()
	require.NoError(t, err)
	require.Positive(t, nextGW)

	var cachedGW int
	require.True(t, endpoints.GetSharedCache().Get("next_gameweek", &cachedGW))
	require.Equal(t, nextGW, cachedGW)

	require.EqualValues(t, 1, bootstrapFetches.Load(),
		"all bootstrap sections must be served by a single upstream fetch")
}

// Clients sharing a store deliberately share the derived next-gameweek key,
// just like the underlying gameweeks list. Separate stores remain independent.
func TestGetNextGameWeek_SharedCache(t *testing.T) {
	store := cache.NewMemoryCache()
	var hitsA, hitsB atomic.Int64
	serverA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitsA.Add(1)
		if _, err := w.Write([]byte(`{"events":[{"id":7,"is_next":true}]}`)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	t.Cleanup(serverA.Close)
	serverB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitsB.Add(1)
		if _, err := w.Write([]byte(`{"events":[{"id":8,"is_next":true}]}`)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	t.Cleanup(serverB.Close)

	a, err := client.NewClient(client.WithBaseURL(serverA.URL), client.WithCache(store))
	require.NoError(t, err)
	first, err := a.Bootstrap.GetNextGameWeek()
	require.NoError(t, err)
	require.Equal(t, 7, first)
	var cached int
	require.True(t, store.Get("next_gameweek", &cached))
	require.Equal(t, first, cached)
	require.False(t, store.Get("next_gameweek:"+serverA.URL, &cached))

	// Remove the source list so the second call must reuse the derived key.
	store.Delete("gameweeks")
	b, err := client.NewClient(client.WithBaseURL(serverB.URL), client.WithCache(store))
	require.NoError(t, err)
	second, err := b.Bootstrap.GetNextGameWeekWithContext(context.Background())
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.EqualValues(t, 1, hitsA.Load())
	require.Zero(t, hitsB.Load())

	independent, err := client.NewClient(client.WithBaseURL(serverB.URL), client.WithCache(cache.NewMemoryCache()))
	require.NoError(t, err)
	other, err := independent.Bootstrap.GetNextGameWeek()
	require.NoError(t, err)
	require.Equal(t, 8, other)
	require.EqualValues(t, 1, hitsB.Load())
}

func TestBootstrapGameweekHelpersAndContext(t *testing.T) {
	memCache := cache.NewMemoryCache()
	endpoints.SetSharedCache(memCache)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/bootstrap-static/" {
			writeTestdata(t, w, "bootstrap-static.json")
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	c, err := client.NewClient(
		client.WithBaseURL(server.URL),
		freshMemoryCache(t),
	)
	require.NoError(t, err)

	ctx := context.Background()

	// GetCurrentGameWeekWithContext
	currentGW, err := c.Bootstrap.GetCurrentGameWeekWithContext(ctx)
	require.NoError(t, err)
	require.Positive(t, currentGW)

	// GetNextGameWeekWithContext
	nextGW, err := c.Bootstrap.GetNextGameWeekWithContext(ctx)
	require.NoError(t, err)
	require.Positive(t, nextGW)

	// GetNextGameWeekModel
	nextModel, err := c.Bootstrap.GetNextGameWeekModel(ctx)
	require.NoError(t, err)
	require.NotNil(t, nextModel)
	require.Equal(t, nextGW, nextModel.ID)
	require.True(t, nextModel.IsNext)

	// GetUpcomingGameWeeks
	upcoming, err := c.Bootstrap.GetUpcomingGameWeeks(ctx, 3)
	require.NoError(t, err)
	require.NotEmpty(t, upcoming)
	require.LessOrEqual(t, len(upcoming), 3)
	for _, gw := range upcoming {
		require.True(t, gw.IsUpcoming())
	}

	// Canceled context should return error
	canceledCtx, cancel := context.WithCancel(ctx)
	cancel()
	memCacheCanceled := cache.NewMemoryCache()
	endpoints.SetSharedCache(memCacheCanceled)
	_, err = c.Bootstrap.GetPlayersWithContext(canceledCtx)
	require.Error(t, err)
}
