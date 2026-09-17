package endpoints_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AbdoAnss/go-fantasy-pl/client"
	"github.com/AbdoAnss/go-fantasy-pl/endpoints"
	"github.com/AbdoAnss/go-fantasy-pl/internal/cache"
	"github.com/AbdoAnss/go-fantasy-pl/models"
	"github.com/stretchr/testify/require"
)

func TestManagerPicksSharingIsolationAndRollover(t *testing.T) {
	store := cache.NewMemoryCache()
	setGW := func(s cache.Cache, gw int) {
		require.NoError(t, s.Set("gameweeks", []models.GameWeek{{ID: gw, IsCurrent: true}}, time.Hour))
	}
	setGW(store, 7)
	// This fresh obsolete entry must neither be read nor deleted.
	require.NoError(t, store.Set("manager_team_123", models.ManagerTeam{}, time.Hour))
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var manager, gw int
		if _, err := fmt.Sscanf(r.URL.Path, "/entry/%d/event/%d/picks/", &manager, &gw); err != nil {
			http.NotFound(w, r)
			return
		}
		hits.Add(1)
		_, _ = fmt.Fprintf(w, `{"picks":[{"element":%d}],"entry_history":{"event":%d}}`, manager, gw)
	}))
	t.Cleanup(server.Close)
	makeClient := func(s cache.Cache) *client.Client {
		c, err := client.NewClient(client.WithBaseURL(server.URL), client.WithCache(s))
		require.NoError(t, err)
		return c
	}
	a, b := makeClient(store), makeClient(store)
	first, err := a.Managers.GetCurrentTeam(123)
	require.NoError(t, err)
	require.Equal(t, 7, first.EntryHistory.Event)
	require.Equal(t, 123, first.Picks[0].Element)
	first.Picks[0].Element = 999 // Returned-data mutation must not poison shared data.
	second, err := b.Managers.GetCurrentTeam(123)
	require.NoError(t, err)
	require.Equal(t, 123, second.Picks[0].Element)
	require.EqualValues(t, 1, hits.Load())
	second.Picks[0].Element = 888
	again, err := a.Managers.GetCurrentTeam(123)
	require.NoError(t, err)
	require.Equal(t, 123, again.Picks[0].Element)
	other, err := b.Managers.GetCurrentTeam(456)
	require.NoError(t, err)
	require.Equal(t, 456, other.Picks[0].Element)
	require.EqualValues(t, 2, hits.Load())

	setGW(store, 8) // Deterministic metadata rollover while GW7 picks remain fresh.
	next, err := b.Managers.GetCurrentTeam(123)
	require.NoError(t, err)
	require.Equal(t, 8, next.EntryHistory.Event)
	require.EqualValues(t, 3, hits.Load())
	var old models.ManagerTeam
	require.True(t, store.Get("manager_team_123_gw7", &old))
	require.Equal(t, 7, old.EntryHistory.Event)
	require.True(t, store.Get("manager_team_123_gw8", &old))
	require.Equal(t, 8, old.EntryHistory.Event)
	require.True(t, store.Get("manager_team_123", &old), "migration must not clear old entries")

	isolated := cache.NewMemoryCache()
	setGW(isolated, 8)
	_, err = makeClient(isolated).Managers.GetCurrentTeam(123)
	require.NoError(t, err)
	require.EqualValues(t, 4, hits.Load(), "separate stores must not reuse picks")
}

func TestManagerPicksPreseason(t *testing.T) {
	for _, events := range []string{`[]`, `[{"id":1,"is_next":true}]`} {
		t.Run(events, func(t *testing.T) {
			store := cache.NewMemoryCache()
			require.NoError(t, store.Set("manager_team_123", models.ManagerTeam{}, time.Hour))
			require.NoError(t, store.Set("manager_team_123_gw1", models.ManagerTeam{}, time.Hour))
			var bootstrapHits, picksHits atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/bootstrap-static/" {
					bootstrapHits.Add(1)
					_, _ = fmt.Fprintf(w, `{"events":%s}`, events)
					return
				}
				picksHits.Add(1)
				http.NotFound(w, r)
			}))
			t.Cleanup(server.Close)
			c, err := client.NewClient(client.WithBaseURL(server.URL), client.WithCache(store))
			require.NoError(t, err)
			for range 2 {
				team, err := c.Managers.GetCurrentTeam(123)
				require.Nil(t, team)
				require.ErrorContains(t, err, "failed to find current gameweek")
			}
			require.EqualValues(t, 1, bootstrapHits.Load())
			require.Zero(t, picksHits.Load(), "no picks fetch without a current GW")
		})
	}
}

func TestSDKEndpointIgnoresLegacyReplacement(t *testing.T) {
	old := endpoints.GetSharedCache()
	t.Cleanup(func() { endpoints.SetSharedCache(old) })
	c := newManagerServer(t, 123)
	first, err := c.Managers.GetCurrentTeam(123)
	require.NoError(t, err)
	replacement := cache.NewMemoryCache()
	require.NoError(t, replacement.Set("gameweeks", []models.GameWeek{{ID: 99, IsCurrent: true}}, time.Hour))
	endpoints.SetSharedCache(replacement)
	second, err := c.Managers.GetCurrentTeam(123)
	require.NoError(t, err)
	require.Equal(t, first, second)
	var picks models.ManagerTeam
	require.False(t, replacement.Get("manager_team_123_gw24", &picks))
}
