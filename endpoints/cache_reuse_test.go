package endpoints_test

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/AbdoAnss/go-fantasy-pl/client"
	"github.com/AbdoAnss/go-fantasy-pl/endpoints"
	"github.com/stretchr/testify/require"
)

func TestMemoryCacheReuseAfterClientConstruction(t *testing.T) {
	for _, mode := range []string{"option", "environment"} {
		t.Run(mode, func(t *testing.T) {
			old := endpoints.GetSharedCache()
			t.Cleanup(func() { endpoints.SetSharedCache(old) })
			t.Setenv("FPL_CACHE_BACKEND", "memory")
			var hits atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				_, _ = w.Write([]byte(`{"elements":[{"id":31}]}`))
			}))
			t.Cleanup(server.Close)
			opts := []client.Option{client.WithBaseURL(server.URL)}
			if mode == "option" {
				opts = append(opts, client.WithMemoryCache())
			}
			a, err := client.NewClient(opts...)
			require.NoError(t, err)
			endpoints.GetSharedCache().Clear()
			t.Cleanup(func() { endpoints.GetSharedCache().Clear() })
			first, err := a.Players.GetAllPlayers()
			require.NoError(t, err)
			b, err := client.NewClient(opts...)
			require.NoError(t, err)
			second, err := b.Players.GetAllPlayers()
			require.NoError(t, err)
			require.Equal(t, first, second)
			require.EqualValues(t, 1, hits.Load())
		})
	}
}
