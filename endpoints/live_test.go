package endpoints_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/AbdoAnss/go-fantasy-pl/client"
	"github.com/AbdoAnss/go-fantasy-pl/endpoints"
	"github.com/AbdoAnss/go-fantasy-pl/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func liveEventElements(t *testing.T) []models.LiveElement {
	t.Helper()

	var live models.EventLive
	require.NoError(t, json.Unmarshal(readTestdata(t, "live-1.json"), &live))
	require.NotEmpty(t, live.Elements)
	return live.Elements
}

func TestGetEventLive(t *testing.T) {
	c, server := newEndpointTestClient(t)
	defer server.Close()

	live, err := c.Live.GetEventLive(1)
	require.NoError(t, err)
	require.NotNil(t, live)

	want := liveEventElements(t)
	require.Len(t, live.Elements, len(want))

	played := 0
	for _, el := range live.Elements {
		assert.Positive(t, el.ID)
		assert.GreaterOrEqual(t, el.Stats.TotalPoints, -15, "totals can be negative (red cards, own goals) but not unboundedly")
		assert.GreaterOrEqual(t, el.Stats.Bps, -30, "BPS can go negative via red-card deductions")
		// Bonus is capped per fixture but aggregates across every fixture in
		// the gameweek, so bound it by the number of fixtures the element
		// explains: double gameweeks can legitimately report up to 3 each.
		fixturesFeatured := len(el.Explain)
		if fixturesFeatured == 0 {
			fixturesFeatured = 1
		}
		assert.LessOrEqual(t, el.Stats.Bonus, 3*fixturesFeatured,
			"bonus must not exceed 3 per fixture featured")
		for _, fx := range el.Explain {
			assert.Positive(t, fx.Fixture)
			require.NotEmpty(t, fx.Stats)
		}
		if el.Stats.Played {
			played++
			// A player who took the pitch must have logged minutes and an
			// explain entry for every fixture they featured in.
			assert.Positive(t, el.Stats.Minutes)
			assert.NotEmpty(t, el.Explain)
		}
	}
	assert.Positive(t, played, "a captured mid/final gameweek must have played players")
}

func TestGetEventLiveCaching(t *testing.T) {
	// A counting handler lets us prove the cache actually short-circuits;
	// serving identical bytes alone cannot distinguish a cached second call
	// from an uncached one.
	var fetches atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/event/1/live/" {
			fetches.Add(1)
			writeTestdata(t, w, "live-1.json")
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	c, err := client.NewClient(
		client.WithBaseURL(server.URL),
		freshMemoryCache(t),
	)
	require.NoError(t, err)

	first, err := c.Live.GetEventLive(1)
	require.NoError(t, err)
	assert.EqualValues(t, 1, fetches.Load(), "first call should hit upstream")

	second, err := c.Live.GetEventLive(1)
	require.NoError(t, err)

	assert.Equal(t, first, second, "cached result should be identical")
	assert.EqualValues(t, 1, fetches.Load(), "second call must be served from cache, not upstream")
}

func TestGetNonExistentEventLive(t *testing.T) {
	c, server := newEndpointTestClient(t)
	defer server.Close()

	live, err := c.Live.GetEventLive(99)
	require.Error(t, err)
	assert.Nil(t, live)

	var notFound *endpoints.EventLiveNotFoundError
	assert.ErrorAs(t, err, &notFound)
	assert.Equal(t, 99, notFound.EventID)
}

func TestLiveElementHelpers(t *testing.T) {
	c, server := newEndpointTestClient(t)
	defer server.Close()

	live, err := c.Live.GetEventLive(1)
	require.NoError(t, err)

	// Find a player with a nonzero string-typed decimal to exercise parsing.
	var scorer *models.LiveElementStats
	for i := range live.Elements {
		if live.Elements[i].Stats.TotalPoints > 8 {
			scorer = &live.Elements[i].Stats
			break
		}
	}
	require.NotNil(t, scorer, "capture should contain a high-scoring player")

	// The capture's decimal fields are real numbers serialized as strings,
	// so at least one of ICT/xG/xA must parse to something positive.
	total := scorer.GetICTIndex() + scorer.GetExpectedGoals() + scorer.GetExpectedAssists() +
		scorer.GetExpectedGoalInvolvements() + scorer.GetExpectedGoalsConceded()
	assert.Positive(t, total)

	// Lookup helpers.
	id := live.Elements[0].ID
	stats, ok := live.StatsFor(id)
	require.True(t, ok)
	assert.Equal(t, live.Elements[0].Stats, *stats)

	points, ok := live.PointsFor(id)
	require.True(t, ok)
	assert.Equal(t, live.Elements[0].Stats.TotalPoints, points)

	explain, ok := live.ExplainFor(id)
	require.True(t, ok)
	assert.Len(t, explain, len(live.Elements[0].Explain))

	_, ok = live.StatsFor(-1)
	assert.False(t, ok, "unknown player IDs must report not-found")

	// Explain breakdown should sum (with modifications) to the player total.
	sum := 0.0
	for _, fx := range live.Elements[0].Explain {
		for _, st := range fx.Stats {
			sum += st.Points + st.PointsModification
		}
	}
	assert.InDelta(t, float64(live.Elements[0].Stats.TotalPoints), sum, 0.001,
		"explain point contributions should add up to total_points")
}

func TestGetEventLiveAsync(t *testing.T) {
	c, server := newEndpointTestClient(t)
	defer server.Close()

	ch := c.Live.GetEventLiveAsync(t.Context(), 1)
	res := <-ch
	require.NoError(t, res.Err)
	require.NotNil(t, res.Value)
	assert.NotEmpty(t, res.Value.Elements)
}
