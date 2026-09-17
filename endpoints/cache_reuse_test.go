package endpoints_test

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/AbdoAnss/go-fantasy-pl/client"
	"github.com/stretchr/testify/require"
)

func TestMemoryCacheReuseAfterClientConstruction(t *testing.T) {
	for _, mode := range []string{"option", "environment"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("FPL_CACHE_BACKEND", "memory")
			var hits atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				if _, err := w.Write([]byte(`[{"id":31}]`)); err != nil {
					t.Errorf("write upstream response: %v", err)
				}
			}))
			t.Cleanup(server.Close)
			opts := []client.Option{client.WithBaseURL(server.URL)}
			if mode == "option" {
				opts = append(opts, client.WithMemoryCache())
			}
			a, err := client.NewClient(opts...)
			require.NoError(t, err)
			a.Cache().Clear()
			t.Cleanup(func() { a.Cache().Clear() })
			first, err := a.Fixtures.GetAllFixtures()
			require.NoError(t, err)
			b, err := client.NewClient(opts...)
			require.NoError(t, err)
			second, err := b.Fixtures.GetAllFixtures()
			require.NoError(t, err)
			require.Equal(t, first, second)
			require.EqualValues(t, 1, hits.Load())
		})
	}
}
