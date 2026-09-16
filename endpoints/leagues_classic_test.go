package endpoints_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AbdoAnss/go-fantasy-pl/client"
	"github.com/AbdoAnss/go-fantasy-pl/endpoints"
	"github.com/AbdoAnss/go-fantasy-pl/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newLeagueServer serves canned responses for a single league endpoint,
// letting each subtest pick the failure mode.
func newLeagueServer(t *testing.T, handler http.HandlerFunc) *client.Client {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	c, err := client.NewClient(client.WithBaseURL(server.URL), freshMemoryCache(t))
	require.NoError(t, err)
	return c
}

func TestGetClassicLeagueStandings(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		c := newLeagueServer(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/leagues-classic/313/standings/", r.URL.Path)
			assert.Equal(t, "1", r.URL.Query().Get("page_standings"))

			w.Header().Set("Content-Type", "application/json")
			writeTestdata(t, w, "leagues-classic-standings.json")
		})

		league, err := c.Leagues.GetClassicLeagueStandings(313, 1)
		require.NoError(t, err)
		assert.Equal(t, 313, league.League.ID, "league ID should echo the request")
		assert.NotEmpty(t, league.League.Name)
	})

	t.Run("not found wraps sentinel", func(t *testing.T) {
		c := newLeagueServer(t, http.NotFound)

		league, err := c.Leagues.GetClassicLeagueStandings(99999999, 1)
		assert.Nil(t, league)
		require.Error(t, err)
		assert.ErrorContains(t, err, "league with ID 99999999 not found")
		assert.ErrorIs(t, err, endpoints.ErrLeagueNotFound)
	})

	t.Run("malformed json", func(t *testing.T) {
		c := newLeagueServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("{"))
		})

		league, err := c.Leagues.GetClassicLeagueStandings(313, 1)
		assert.Nil(t, league)
		assert.ErrorContains(t, err, "failed to decode league data")
	})

	t.Run("invalid league id in payload", func(t *testing.T) {
		c := newLeagueServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"league": {"id": 0}}`))
		})

		league, err := c.Leagues.GetClassicLeagueStandings(313, 1)
		assert.Nil(t, league)
		assert.EqualError(t, err, "invalid league ID")
	})

	t.Run("unexpected status", func(t *testing.T) {
		c := newLeagueServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTeapot)
		})

		league, err := c.Leagues.GetClassicLeagueStandings(313, 1)
		assert.Nil(t, league)
		assert.EqualError(t, err, "unexpected status code: 418")
	})
}

func TestGetH2HLeagueStandings_Errors(t *testing.T) {
	t.Run("invalid input", func(t *testing.T) {
		c, err := client.NewClient(freshMemoryCache(t))
		require.NoError(t, err)

		standings, err := c.Leagues.GetH2HLeagueStandings(0, 1)
		assert.Nil(t, standings)
		assert.EqualError(t, err, "league ID must be positive")

		standings, err = c.Leagues.GetH2HLeagueStandings(1221170, 0)
		assert.Nil(t, standings)
		assert.EqualError(t, err, "page must be positive")
	})

	t.Run("not found wraps sentinel", func(t *testing.T) {
		c := newLeagueServer(t, http.NotFound)

		standings, err := c.Leagues.GetH2HLeagueStandings(1221170, 1)
		assert.Nil(t, standings)
		assert.ErrorContains(t, err, "league with ID 1221170 not found")
		assert.ErrorIs(t, err, endpoints.ErrLeagueNotFound)
	})

	t.Run("malformed json", func(t *testing.T) {
		c := newLeagueServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"league": `))
		})

		standings, err := c.Leagues.GetH2HLeagueStandings(1221170, 1)
		assert.Nil(t, standings)
		assert.ErrorContains(t, err, "failed to decode H2H league standings data")
	})

	t.Run("invalid league id in payload", func(t *testing.T) {
		c := newLeagueServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"league": {"id": 0}}`))
		})

		standings, err := c.Leagues.GetH2HLeagueStandings(1221170, 1)
		assert.Nil(t, standings)
		assert.EqualError(t, err, "invalid league ID")
	})

	t.Run("unexpected status", func(t *testing.T) {
		c := newLeagueServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTeapot)
		})

		standings, err := c.Leagues.GetH2HLeagueStandings(1221170, 1)
		assert.Nil(t, standings)
		assert.EqualError(t, err, "unexpected status code: 418")
	})
}

func TestGetTotalPages(t *testing.T) {
	c, err := client.NewClient(freshMemoryCache(t))
	require.NoError(t, err)

	assert.Equal(t, 0, c.Leagues.GetTotalPages(nil))
	assert.Equal(t, 0, c.Leagues.GetTotalPages(&models.ClassicLeague{}))

	// Fewer than 50 entries still occupies one page.
	small := 10
	assert.Equal(t, 1, c.Leagues.GetTotalPages(&models.ClassicLeague{League: models.League{MaxEntries: &small}}))

	// MaxEntries drives pagination when set (50 per page).
	large := 120
	assert.Equal(t, 3, c.Leagues.GetTotalPages(&models.ClassicLeague{League: models.League{MaxEntries: &large}}))

	// Without MaxEntries, fall back to the number of standings rows.
	rows := &models.ClassicLeague{
		Standings: models.Standings{Results: make([]models.LeagueManager, 60)},
	}
	assert.Equal(t, 2, c.Leagues.GetTotalPages(rows))
}
