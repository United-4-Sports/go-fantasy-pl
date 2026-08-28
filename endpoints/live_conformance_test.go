package endpoints_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/AbdoAnss/go-fantasy-pl/client"
	"github.com/AbdoAnss/go-fantasy-pl/endpoints"
	"github.com/AbdoAnss/go-fantasy-pl/internal/conformance"
	"github.com/AbdoAnss/go-fantasy-pl/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLiveConformance is the automatic live-API verification loop. It reads
// IDs from testdata/live_ids.json, calls every library method for those IDs,
// fetches the raw payloads via GetRaw, and runs the same schema-conformance
// rules the hermetic suite uses — so upstream schema drift is caught by a
// failing test instead of silent zero-values.
//
// Gating:
//   - FPL_LIVE_TEST=1       enables live requests (never runs in CI's PR path)
//   - FPL_RECAPTURE=1       additionally rewrites the hermetic captures in
//     testdata/ from the live payloads (implies live)
//
// The FPL API purges leagues between seasons: a 404 for a configured league
// or manager is treated as a stale ID and skipped, not failed. Update
// testdata/live_ids.json at the start of each season.
func TestLiveConformance(t *testing.T) {
	skipUnlessLive(t)

	recapture := os.Getenv("FPL_RECAPTURE") == "1"

	c, err := client.NewClient(
		client.WithMemoryCache(),
		// The walk below makes ~35 requests; keep it comfortably inside the
		// bucket while staying polite to the undocumented public API.
		client.WithRateLimit(100, time.Minute),
	)
	require.NoError(t, err)

	ids := loadLiveIDs(t)

	var currentGameweek int
	if gw, err := c.Bootstrap.GetCurrentGameWeek(); err == nil {
		currentGameweek = gw
	} else {
		t.Log("no current gameweek (pre-season); gameweek-dependent cases will be skipped")
	}

	t.Run("Bootstrap", func(t *testing.T) {
		raw := mustGetRaw(t, c, "/bootstrap-static/")

		var resp endpoints.Response
		require.NoError(t, json.Unmarshal(raw, &resp))

		conformance.Check(t, raw, conformance.Spec{Model: &resp, Allowlist: bootstrapAllowlist})
		require.NotEmpty(t, resp.Teams)
		conformance.Check(t, conformance.Extract(t, raw, "teams", 0),
			conformance.Spec{Model: &resp.Teams[0], Allowlist: teamAllowlist})
		require.NotEmpty(t, resp.Elements)
		conformance.Check(t, conformance.Extract(t, raw, "elements", 0),
			conformance.Spec{Model: &resp.Elements[0], Allowlist: playerAllowlist})
		require.NotEmpty(t, resp.Events)
		conformance.Check(t, conformance.Extract(t, raw, "events", 0),
			conformance.Spec{Model: &resp.Events[0], Allowlist: gameWeekAllowlist})
		conformance.Check(t, conformance.Extract(t, raw, "game_settings"),
			conformance.Spec{Model: &resp.Settings, Allowlist: gameSettingsAllowlist})

		logUnmappedBacklog(t, "Player", conformance.Extract(t, raw, "elements", 0),
			conformance.Spec{Model: &resp.Elements[0], Allowlist: playerAllowlist})

		// Library behavior.
		teams, err := c.Bootstrap.GetTeams()
		require.NoError(t, err)
		assert.NotEmpty(t, teams)

		players, err := c.Bootstrap.GetPlayers()
		require.NoError(t, err)
		assert.NotEmpty(t, players)

		gameweeks, err := c.Bootstrap.GetGameWeeks()
		require.NoError(t, err)
		assert.NotEmpty(t, gameweeks)

		settings, err := c.Bootstrap.GetSettings()
		require.NoError(t, err)
		assert.NotNil(t, settings)

		if recapture {
			writeTestdataFile(t, "bootstrap-static.json", raw)
		}
	})

	t.Run("Fixtures", func(t *testing.T) {
		raw := mustGetRaw(t, c, "/fixtures/")

		var fixtures []models.Fixture
		require.NoError(t, json.Unmarshal(raw, &fixtures))
		require.NotEmpty(t, fixtures)

		conformance.Check(t, raw, conformance.Spec{Model: fixtures})

		// Library behavior.
		all, err := c.Fixtures.GetAllFixtures()
		require.NoError(t, err)
		assert.NotEmpty(t, all)

		first, err := c.Fixtures.GetFixture(fixtures[0].ID)
		require.NoError(t, err)
		assert.Equal(t, fixtures[0].ID, first.ID, "returned fixture should echo the requested ID")

		if recapture {
			writeTestdataFile(t, "fixtures.json", raw)
		}
	})

	for _, id := range ids.Players {
		t.Run(fmt.Sprintf("Player%d", id), func(t *testing.T) {
			raw := mustGetRaw(t, c, fmt.Sprintf("/element-summary/%d/", id))

			var history models.PlayerHistory
			require.NoError(t, json.Unmarshal(raw, &history))
			conformance.Check(t, raw, conformance.Spec{Model: &history})

			if len(history.History) > 0 {
				conformance.Check(t, conformance.Extract(t, raw, "history", 0),
					conformance.Spec{Model: &history.History[0]})
			}
			if len(history.HistoryPast) > 0 {
				conformance.Check(t, conformance.Extract(t, raw, "history_past", 0),
					conformance.Spec{Model: &history.HistoryPast[0], Allowlist: pastHistoryStatsAllowlist})
			}
			if len(history.Fixtures) > 0 {
				conformance.Check(t, conformance.Extract(t, raw, "fixtures", 0),
					conformance.Spec{Model: &history.Fixtures[0]})
			}

			// Library behavior.
			h, err := c.Players.GetPlayerHistory(id)
			require.NoError(t, err)
			assert.NotNil(t, h)

			p, err := c.Players.GetPlayer(id)
			require.NoError(t, err)
			assert.Equal(t, id, p.ID, "returned player should echo the requested ID")

			if recapture {
				writeTestdataFile(t, jsonName("element-summary-%d.json", id), raw)
			}
		})
	}

	t.Run("Teams", func(t *testing.T) {
		teams, err := c.Bootstrap.GetTeams()
		require.NoError(t, err)
		require.NotEmpty(t, teams)

		// Sample three teams; IDs are dense 1-20 every season.
		sample := min(len(teams), 3)
		for _, tm := range teams[:sample] {
			got, err := c.Teams.GetTeam(tm.ID)
			require.NoError(t, err)
			assert.Equal(t, tm.ID, got.ID, "returned team should echo the requested ID")
		}
	})

	for _, id := range ids.Managers {
		t.Run(fmt.Sprintf("Manager%d", id), func(t *testing.T) {
			// Raw fetch first so a purged entry skips before any hard
			// assertions run.
			raw, rawErr := c.GetRaw(fmt.Sprintf("/entry/%d/", id))
			if isStalePayload(t, rawErr) {
				t.Skipf("manager %d no longer exists on the live API", id)
			}
			require.NoError(t, rawErr)
			require.NotEmpty(t, raw)

			var manager models.Manager
			require.NoError(t, json.Unmarshal(raw, &manager))
			conformance.Check(t, raw, conformance.Spec{Model: &manager, Allowlist: managerAllowlist})

			// Library behavior.
			m, err := c.Managers.GetManager(id)
			if isStaleID(t, err) {
				t.Skipf("manager %d no longer exists on the live API", id)
			}
			require.NoError(t, err)
			require.NotNil(t, m.ID)
			assert.Equal(t, id, *m.ID, "returned manager should echo the requested ID")

			// History.
			histRaw := mustGetRaw(t, c, fmt.Sprintf("/entry/%d/history", id))
			var managerHistory models.ManagerHistory
			require.NoError(t, json.Unmarshal(histRaw, &managerHistory))
			conformance.Check(t, histRaw, conformance.Spec{Model: &managerHistory})

			_, err = c.Managers.GetManagerHistory(id)
			require.NoError(t, err)

			// Current team only exists once a gameweek is in progress.
			if currentGameweek == 0 {
				t.Skip("no current gameweek yet; skipping picks checks")
			}
			team, err := c.Managers.GetCurrentTeam(id)
			require.NoError(t, err)
			assert.NotEmpty(t, team.Picks)
		})
	}

	for _, id := range ids.ClassicLeagues {
		t.Run(fmt.Sprintf("ClassicLeague%d", id), func(t *testing.T) {
			// The library call first: 404 means the league was purged between
			// seasons, which is a skip rather than a failure.
			league, err := c.Leagues.GetClassicLeagueStandings(id, 1)
			if isStaleID(t, err) {
				t.Skipf("classic league %d no longer exists on the live API", id)
			}
			require.NoError(t, err)

			raw := mustGetRaw(t, c, fmt.Sprintf("/leagues-classic/%d/standings/?page_standings=1", id))

			var cl models.ClassicLeague
			require.NoError(t, json.Unmarshal(raw, &cl))
			conformance.Check(t, raw, conformance.Spec{Model: &cl})

			if len(cl.Standings.Results) > 0 {
				conformance.Check(t, conformance.Extract(t, raw, "standings", "results", 0),
					conformance.Spec{Model: &cl.Standings.Results[0]})
			}

			assert.Equal(t, id, cl.League.ID, "league ID should echo the request")
			assert.NotEmpty(t, league.League.Name)
		})
	}

	for _, id := range ids.H2HLeagues {
		t.Run(fmt.Sprintf("H2HLeague%d", id), func(t *testing.T) {
			feed, err := c.Leagues.GetH2HLeagueMatches(id, 1, 0)
			if isStaleID(t, err) {
				t.Skipf("H2H league %d no longer exists on the live API", id)
			}
			require.NoError(t, err)
			require.NotNil(t, feed)

			raw := mustGetRaw(t, c, fmt.Sprintf("/leagues-h2h-matches/league/%d/?page=1", id))

			var page models.H2HLeagueMatchesPage
			require.NoError(t, json.Unmarshal(raw, &page))
			conformance.Check(t, raw, conformance.Spec{Model: &page})

			if len(page.Results) > 0 {
				conformance.Check(t, conformance.Extract(t, raw, "results", 0),
					conformance.Spec{Model: &page.Results[0]})

				// Event filtering invariant.
				filtered, err := c.Leagues.GetH2HLeagueMatches(id, 1, 1)
				require.NoError(t, err)
				for _, m := range filtered.Results {
					assert.Equal(t, 1, m.Event, "filtered feed must only contain the requested gameweek")
				}
			}

			// Invalid event is rejected with a domain error.
			_, err = c.Leagues.GetH2HLeagueMatches(id, 1, 999)
			var invalidQuery *endpoints.ErrInvalidH2HQuery
			require.True(t, errors.As(err, &invalidQuery),
				"expected *ErrInvalidH2HQuery for event=999, got: %v", err)
		})
	}

	t.Run("Live", func(t *testing.T) {
		eventID := currentGameweek
		if eventID == 0 {
			eventID = 1 // pre-season fallback: GW1 always exists
		}
		raw := mustGetRaw(t, c, fmt.Sprintf("/event/%d/live/", eventID))

		var live models.EventLive
		require.NoError(t, json.Unmarshal(raw, &live))
		require.NotEmpty(t, live.Elements)

		conformance.Check(t, raw, conformance.Spec{Model: &live, Allowlist: eventLiveAllowlist})
		conformance.Check(t, conformance.Extract(t, raw, "elements", 0),
			conformance.Spec{Model: &live.Elements[0], Allowlist: eventLiveAllowlist})

		// Check only descends one array level, so validate the explain tree
		// of the first element explicitly as well.
		if explain := live.Elements[0].Explain; len(explain) > 0 {
			conformance.Check(t, conformance.Extract(t, raw, "elements", 0, "explain", 0),
				conformance.Spec{Model: &explain[0], Allowlist: eventLiveAllowlist})
			if len(explain[0].Stats) > 0 {
				conformance.Check(t, conformance.Extract(t, raw, "elements", 0, "explain", 0, "stats", 0),
					conformance.Spec{Model: &explain[0].Stats[0], Allowlist: eventLiveAllowlist})
			}
		}

		// Explain breakdowns must sum (with modifications) to total_points.
		for _, el := range live.Elements {
			sum := 0.0
			for _, fx := range el.Explain {
				for _, st := range fx.Stats {
					sum += st.Points + st.PointsModification
				}
			}
			assert.InDeltaf(t, float64(el.Stats.TotalPoints), sum, 0.001,
				"player %d explain contributions should add up to total_points", el.ID)
		}

		// Library behavior: fetch through the service and assert internal
		// consistency of that single snapshot. Two separate upstream fetches
		// can straddle a goal/bonus update mid-match, so cross-fetch equality
		// would be a race, not an invariant.
		got, err := c.Live.GetEventLive(eventID)
		require.NoError(t, err)
		require.Len(t, got.Elements, len(live.Elements))

		first := got.Elements[0].ID
		points, ok := got.PointsFor(first)
		require.True(t, ok, "PointsFor should find the first element")
		assert.Equal(t, got.Elements[0].Stats.TotalPoints, points,
			"PointsFor should echo the API total for that player")

		// Out-of-range gameweeks produce the typed domain error.
		_, err = c.Live.GetEventLive(999)
		var notFound *endpoints.EventLiveNotFoundError
		assert.ErrorAs(t, err, &notFound)

		if recapture {
			writeTestdataFile(t, jsonName("live-%d.json", eventID), raw)
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		_, err := c.Managers.GetManager(99999999)
		assert.Error(t, err)

		_, err = c.Leagues.GetClassicLeagueStandings(99999999, 1)
		assert.Error(t, err)
		assert.ErrorIs(t, err, endpoints.ErrLeagueNotFound)

		_, err = c.Fixtures.GetFixture(999999)
		assert.Error(t, err)

		_, err = c.Players.GetPlayer(9999)
		assert.Error(t, err)
	})
}

// liveIDs is the manually curated ID list for the live walk. Teams, players,
// and fixtures are derived from bootstrap; only managers and leagues require
// seasonal maintenance (the API purges leagues between seasons).
type liveIDs struct {
	Managers       []int `json:"managers"`
	Players        []int `json:"players"`
	ClassicLeagues []int `json:"classic_leagues"`
	H2HLeagues     []int `json:"h2h_leagues"`
}

func loadLiveIDs(t *testing.T) liveIDs {
	t.Helper()

	var ids liveIDs
	require.NoError(t, json.Unmarshal(readTestdata(t, "live_ids.json"), &ids))
	return ids
}

func mustGetRaw(t *testing.T, c *client.Client, endpoint string) []byte {
	t.Helper()

	raw, err := c.GetRaw(endpoint)
	require.NoError(t, err)
	require.NotEmpty(t, raw)
	return raw
}

// isStaleID reports whether err is a not-found error for a configured live
// ID, meaning the entity was purged (typically between seasons).
func isStaleID(t *testing.T, err error) bool {
	t.Helper()

	if err == nil {
		return false
	}
	if errors.Is(err, endpoints.ErrLeagueNotFound) {
		return true
	}
	return false
}

// isStalePayload reports whether a GetRaw error is a 404 for a configured
// live ID, so purged entities skip instead of failing the walk.
func isStalePayload(t *testing.T, err error) bool {
	t.Helper()

	var statusErr *client.StatusError
	return errors.As(err, &statusErr) && statusErr.StatusCode == http.StatusNotFound
}

// logUnmappedBacklog surfaces the "available but unmapped" inventory so the
// allowlists double as a model-enrichment backlog.
func logUnmappedBacklog(t *testing.T, name string, raw []byte, spec conformance.Spec) {
	t.Helper()

	if unmapped := conformance.Report(t, raw, spec); len(unmapped) > 0 {
		t.Logf("%s: %d API keys available but unmapped (see conformance_specs_test.go)", name, len(unmapped))
	}
}
