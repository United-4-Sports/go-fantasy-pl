package endpoints

import (
	"context"
	"testing"
	"time"

	"github.com/AbdoAnss/go-fantasy-pl/internal/cache"
	"github.com/stretchr/testify/require"
)

func TestBootstrapCancelledWaiterDoesNotStrandGate(t *testing.T) {
	// Simulate an owner holding the process-wide gate. No background goroutine
	// is needed, and cleanup releases the slot even if an assertion fails.
	bootstrapGate <- struct{}{}
	held := true
	defer func() {
		if held {
			<-bootstrapGate
		}
	}()

	store := cache.NewMemoryCache()
	service := NewBootstrapService(&changingProvider{
		Client: captureClient{payload: `{"teams":[{"id":1}],"elements":[],"events":[],"game_settings":{}}`},
		first:  store, later: store,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	_, err := service.GetTeamsWithContext(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Len(t, bootstrapGate, 1, "waiter must not release the owner's slot")

	<-bootstrapGate
	held = false
	fresh, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	teams, err := service.GetTeamsWithContext(fresh)
	require.NoError(t, err, "subsequent cold-cache request must acquire the gate")
	require.Len(t, teams, 1)
	require.Empty(t, bootstrapGate, "successful owner must release the slot")
}
