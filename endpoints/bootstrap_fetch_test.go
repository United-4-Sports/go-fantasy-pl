package endpoints_test

import (
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
	endpoints.SetSharedCache(cache.NewMemoryCache())

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

	require.EqualValues(t, 1, bootstrapFetches.Load(),
		"all bootstrap sections must be served by a single upstream fetch")
}
