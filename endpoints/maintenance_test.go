package endpoints_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AbdoAnss/go-fantasy-pl/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestServicesSurfaceMaintenanceAsTypedError pins the contract the live
// conformance harness relies on: while the FPL API is being updated (HTTP 503,
// "The game is being updated."), every service surfaces a *client.MaintenanceError
// through its own error wrapping, so the walk can skip instead of reporting
// schema drift.
func TestServicesSurfaceMaintenanceAsTypedError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`"The game is being updated."`))
	}))
	defer server.Close()

	c, err := client.NewClient(
		client.WithBaseURL(server.URL),
		freshMemoryCache(t),
	)
	require.NoError(t, err)

	t.Run("Manager", func(t *testing.T) {
		_, err := c.Managers.GetManager(1)
		require.Error(t, err)
		assert.True(t, client.IsMaintenance(err), "manager error must stay typed, got: %v", err)
	})

	t.Run("ClassicLeague", func(t *testing.T) {
		_, err := c.Leagues.GetClassicLeagueStandings(1, 1)
		require.Error(t, err)
		assert.True(t, client.IsMaintenance(err), "league error must stay typed, got: %v", err)
	})

	t.Run("Live", func(t *testing.T) {
		_, err := c.Live.GetEventLive(1)
		require.Error(t, err)
		assert.True(t, client.IsMaintenance(err), "live error must stay typed, got: %v", err)
	})
}
