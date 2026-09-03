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
		client.WithMemoryCache(),
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
	require.True(t, endpoints.GetSharedCache().Get("next_gameweek:"+server.URL, &cachedGW))
	require.Equal(t, nextGW, cachedGW)

	require.EqualValues(t, 1, bootstrapFetches.Load(),
		"all bootstrap sections must be served by a single upstream fetch")
}

// TestGetNextGameWeek_CacheScopedToClient ensures next_gameweek cache keys
// are isolated per API client base URL.
func TestGetNextGameWeek_CacheScopedToClient(t *testing.T) {
	t.Setenv("FPL_CACHE_BACKEND", "memory")
	memCache := cache.NewMemoryCache()
	endpoints.SetSharedCache(memCache)

	serverA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		writeTestdata(t, w, "bootstrap-static.json")
	}))
	t.Cleanup(serverA.Close)

	serverB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		writeTestdata(t, w, "bootstrap-static.json")
	}))
	t.Cleanup(serverB.Close)

	clientA, err := client.NewClient(client.WithBaseURL(serverA.URL))
	require.NoError(t, err)

	clientB, err := client.NewClient(client.WithBaseURL(serverB.URL))
	require.NoError(t, err)

	gwA, err := clientA.Bootstrap.GetNextGameWeek()
	require.NoError(t, err)

	gwB, err := clientB.Bootstrap.GetNextGameWeek()
	require.NoError(t, err)

	require.Equal(t, gwA, gwB)

	var valA, valB int
	require.True(t, endpoints.GetSharedCache().Get("next_gameweek:"+serverA.URL, &valA))
	require.True(t, endpoints.GetSharedCache().Get("next_gameweek:"+serverB.URL, &valB))
	require.Equal(t, gwA, valA)
	require.Equal(t, gwB, valB)
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
		client.WithMemoryCache(),
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

