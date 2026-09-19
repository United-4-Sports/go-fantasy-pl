package client_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AbdoAnss/go-fantasy-pl/client"
	"github.com/stretchr/testify/require"
)

// Implements the unchanged legacy Cache API, as an external consumer would.
type cancelingCache struct{ cancel context.CancelFunc }

func (s cancelingCache) Get(string, any) bool               { s.cancel(); return true }
func (cancelingCache) Set(string, any, time.Duration) error { return nil }
func (cancelingCache) Delete(string)                        {}
func (cancelingCache) Clear()                               {}

func TestClientCancellationDuringLegacyCacheRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	c, err := client.NewClient(client.WithBaseURL(server.URL), client.WithCache(cancelingCache{cancel: cancel}))
	require.NoError(t, err)
	_, err = c.Fixtures.GetAllFixturesWithContext(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, hits.Load())
}
