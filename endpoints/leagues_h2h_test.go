package endpoints_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AbdoAnss/go-fantasy-pl/client"
	"github.com/AbdoAnss/go-fantasy-pl/endpoints"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetH2HLeagueMatches(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Our request contract, not API values: correct path and params.
			assert.Equal(t, "/leagues-h2h-matches/league/1221170/", r.URL.Path)
			assert.Equal(t, "1", r.URL.Query().Get("page"))
			assert.Equal(t, "3", r.URL.Query().Get("event"))

			w.Header().Set("Content-Type", "application/json")
			writeTestdata(t, w, "h2h-league-matches.json")
		}))
		defer server.Close()

		c, err := client.NewClient(client.WithBaseURL(server.URL), freshMemoryCache(t))
		require.NoError(t, err)

		resp, err := c.Leagues.GetH2HLeagueMatches(1221170, 1, 3)
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.NotEmpty(t, resp.Results)

		assert.Equal(t, 1, resp.Page, "response should echo the requested page")
	})

	t.Run("invalid input", func(t *testing.T) {
		c, err := client.NewClient(freshMemoryCache(t))
		require.NoError(t, err)

		resp, err := c.Leagues.GetH2HLeagueMatches(0, 1, 0)
		assert.Nil(t, resp)
		assert.EqualError(t, err, "league ID must be positive")

		resp, err = c.Leagues.GetH2HLeagueMatches(1221170, 0, 0)
		assert.Nil(t, resp)
		assert.EqualError(t, err, "page must be positive")

		resp, err = c.Leagues.GetH2HLeagueMatches(1221170, 1, -1)
		assert.Nil(t, resp)
		assert.EqualError(t, err, "event cannot be negative")
	})

	t.Run("not found", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		}))
		defer server.Close()

		c, err := client.NewClient(client.WithBaseURL(server.URL), freshMemoryCache(t))
		require.NoError(t, err)

		resp, err := c.Leagues.GetH2HLeagueMatches(1221170, 1, 0)
		assert.Nil(t, resp)
		assert.ErrorContains(t, err, "league with ID 1221170 not found")
		assert.ErrorIs(t, err, endpoints.ErrLeagueNotFound)
	})

	t.Run("malformed json", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("{"))
		}))
		defer server.Close()

		c, err := client.NewClient(client.WithBaseURL(server.URL), freshMemoryCache(t))
		require.NoError(t, err)

		resp, err := c.Leagues.GetH2HLeagueMatches(1221170, 1, 0)
		assert.Nil(t, resp)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode H2H matches data")
	})

	t.Run("unexpected status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTeapot)
		}))
		defer server.Close()

		c, err := client.NewClient(client.WithBaseURL(server.URL), freshMemoryCache(t))
		require.NoError(t, err)

		resp, err := c.Leagues.GetH2HLeagueMatches(1221170, 1, 0)
		assert.Nil(t, resp)
		assert.EqualError(t, err, "unexpected status code: 418")
	})
}

// newH2HSemanticsServer routes requests to fixtures that exercise the live
// API's event/page semantics. The mixed-feed and event=1 fixtures start from
// a real captured response (h2h-league-matches.json); the knockout fixture
// is synthetic because no live knockout capture was available.
func newH2HSemanticsServer(t *testing.T) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		event := r.URL.Query().Get("event")
		page := r.URL.Query().Get("page")

		var fixture string
		switch {
		case event == "" && page == "1":
			fixture = "h2h-matches-mixed.json"
		case event == "1" && page == "1":
			fixture = "h2h-matches-event1.json"
		case event == "1" && page == "2":
			fixture = "h2h-matches-event1-page2.json"
		case event == "37":
			fixture = "h2h-matches-knockout.json"
		case event == "999":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"detail": "Invalid event."}`))
			return
		default:
			http.NotFound(w, r)
			return
		}
		writeTestdata(t, w, fixture)
	}))
}

// Omitting event returns a paginated mixed feed spanning multiple gameweeks.
func TestGetH2HLeagueMatches_MixedFeedWithoutEvent(t *testing.T) {
	server := newH2HSemanticsServer(t)
	defer server.Close()

	c, err := client.NewClient(client.WithBaseURL(server.URL), freshMemoryCache(t))
	require.NoError(t, err)

	feed, err := c.Leagues.GetH2HLeagueMatches(1221170, 1, 0)
	require.NoError(t, err)
	require.NotEmpty(t, feed.Results)

	events := map[int]bool{}
	for _, m := range feed.Results {
		events[m.Event] = true
	}
	assert.Greater(t, len(events), 1, "mixed feed should span multiple gameweeks, got %v", events)
}

// event=<gw> filters matches to a single gameweek.
func TestGetH2HLeagueMatches_EventFilterSingleGameweek(t *testing.T) {
	server := newH2HSemanticsServer(t)
	defer server.Close()

	c, err := client.NewClient(client.WithBaseURL(server.URL), freshMemoryCache(t))
	require.NoError(t, err)

	feed, err := c.Leagues.GetH2HLeagueMatches(1221170, 1, 1)
	require.NoError(t, err)

	for _, m := range feed.Results {
		assert.Equal(t, 1, m.Event, "filtered feed must only contain the requested gameweek")
	}
}

// An event-filtered page past the last result is empty.
func TestGetH2HLeagueMatches_EmptyFilteredPage(t *testing.T) {
	server := newH2HSemanticsServer(t)
	defer server.Close()

	c, err := client.NewClient(client.WithBaseURL(server.URL), freshMemoryCache(t))
	require.NoError(t, err)

	feed, err := c.Leagues.GetH2HLeagueMatches(1221170, 2, 1)
	require.NoError(t, err)

	assert.Empty(t, feed.Results)
	assert.Equal(t, 2, feed.Page, "response should echo the requested page")
}

// Knockout rounds return is_knockout=true, a populated winner among the two
// entries, and a knockout_name. The fixture is synthetic (see server above).
func TestGetH2HLeagueMatches_KnockoutRound(t *testing.T) {
	server := newH2HSemanticsServer(t)
	defer server.Close()

	c, err := client.NewClient(client.WithBaseURL(server.URL), freshMemoryCache(t))
	require.NoError(t, err)

	feed, err := c.Leagues.GetH2HLeagueMatches(1221170, 1, 37)
	require.NoError(t, err)
	require.NotEmpty(t, feed.Results)

	for _, m := range feed.Results {
		if !m.IsKnockout {
			continue
		}
		require.NotNil(t, m.Winner, "knockout matches must have a winner")
		assert.Contains(t, []int{m.Entry1Entry, m.Entry2Entry}, *m.Winner,
			"knockout winner should be one of the two entries")
		assert.NotEmpty(t, m.KnockoutName)
	}
}

// An event outside the valid gameweek range yields HTTP 400, surfaced as
// *ErrInvalidH2HQuery instead of a generic status-code error.
func TestGetH2HLeagueMatches_InvalidEventReturnsDomainError(t *testing.T) {
	server := newH2HSemanticsServer(t)
	defer server.Close()

	c, err := client.NewClient(client.WithBaseURL(server.URL), freshMemoryCache(t))
	require.NoError(t, err)

	feed, err := c.Leagues.GetH2HLeagueMatches(1221170, 1, 999)
	assert.Nil(t, feed)
	require.Error(t, err)

	var invalidQuery *endpoints.ErrInvalidH2HQuery
	require.True(t, errors.As(err, &invalidQuery),
		"expected *ErrInvalidH2HQuery for event=999, got: %v", err)
	assert.Equal(t, 1221170, invalidQuery.LeagueID)
	assert.Equal(t, 999, invalidQuery.Event)
	assert.NotEmpty(t, invalidQuery.Detail, "the API error detail should be surfaced")
}

func TestGetH2HLeagueStandings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/leagues-h2h/1221170/standings/", r.URL.Path)
		assert.Equal(t, "1", r.URL.Query().Get("page_standings"))

		w.Header().Set("Content-Type", "application/json")
		writeTestdata(t, w, "h2h-league-standings.json")
	}))
	defer server.Close()

	c, err := client.NewClient(client.WithBaseURL(server.URL), freshMemoryCache(t))
	require.NoError(t, err)

	standings, err := c.Leagues.GetH2HLeagueStandings(1221170, 1)
	require.NoError(t, err)
	require.NotNil(t, standings)

	assert.Equal(t, 1221170, standings.League.ID, "league ID should echo the request")
	assert.NotEmpty(t, standings.League.Name)
}
