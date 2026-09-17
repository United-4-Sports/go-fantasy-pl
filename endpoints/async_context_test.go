package endpoints_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AbdoAnss/go-fantasy-pl/client"
	"github.com/AbdoAnss/go-fantasy-pl/endpoints"
	"github.com/AbdoAnss/go-fantasy-pl/internal/cache"
	"github.com/stretchr/testify/require"
)

// Do not receive until a result has been buffered: an abandoned caller must not
// prevent delivery. Check both the result and subsequent channel closure.
func checkAsync[T any](ch <-chan endpoints.Result[T]) func(*testing.T, error) {
	return func(t *testing.T, want error) {
		t.Helper()
		require.Equal(t, 1, cap(ch))
		require.Eventually(t, func() bool { return len(ch) == 1 }, 5*time.Second, time.Millisecond)
		result, ok := <-ch
		require.True(t, ok)
		if want == nil {
			require.NoError(t, result.Err)
		} else {
			require.ErrorIs(t, result.Err, want)
		}
		select {
		case _, ok := <-ch:
			require.False(t, ok, "must deliver exactly one result")
		case <-time.After(5 * time.Second):
			t.Fatal("result channel did not close")
		}
	}
}

var asyncContextCalls = []struct {
	name string
	call func(*client.Client, context.Context) func(*testing.T, error)
}{
	{"players", func(c *client.Client, ctx context.Context) func(*testing.T, error) {
		return checkAsync(c.Players.GetAllPlayersAsync(ctx))
	}},
	{"player history", func(c *client.Client, ctx context.Context) func(*testing.T, error) {
		return checkAsync(c.Players.GetPlayerHistoryAsync(ctx, 1))
	}},
	{"fixtures", func(c *client.Client, ctx context.Context) func(*testing.T, error) {
		return checkAsync(c.Fixtures.GetAllFixturesAsync(ctx))
	}},
	{"teams", func(c *client.Client, ctx context.Context) func(*testing.T, error) {
		return checkAsync(c.Teams.GetAllTeamsAsync(ctx))
	}},
	{"manager", func(c *client.Client, ctx context.Context) func(*testing.T, error) {
		return checkAsync(c.Managers.GetManagerAsync(ctx, 1))
	}},
	{"picks", func(c *client.Client, ctx context.Context) func(*testing.T, error) {
		return checkAsync(c.Managers.GetCurrentTeamAsync(ctx, 1))
	}},
	{"manager history", func(c *client.Client, ctx context.Context) func(*testing.T, error) {
		return checkAsync(c.Managers.GetManagerHistoryAsync(ctx, 1))
	}},
	{"live", func(c *client.Client, ctx context.Context) func(*testing.T, error) {
		return checkAsync(c.Live.GetEventLiveAsync(ctx, 1))
	}},
}

func TestAsyncContextOneResult(t *testing.T) {
	for _, tt := range asyncContextCalls {
		for _, mode := range []string{"nil", "active", "canceled", "deadline"} {
			t.Run(tt.name+"/"+mode, func(t *testing.T) {
				store := &contextStore{Cache: cache.NewMemoryCache()}
				var calls atomic.Int32
				var ctx context.Context
				type key struct{}
				var cancel context.CancelFunc = func() {}
				if mode != "nil" {
					ctx = context.WithValue(context.Background(), key{}, "marker")
				}
				var want error
				if mode == "canceled" {
					ctx, cancel = context.WithCancel(ctx)
					cancel()
					want = context.Canceled
				}
				if mode == "deadline" {
					ctx, cancel = context.WithDeadline(ctx, time.Now().Add(-time.Second))
					want = context.DeadlineExceeded
				}
				defer cancel()
				c, err := client.NewClient(client.WithCache(store), client.WithHTTPClient(&http.Client{Transport: contextTransport(func(r *http.Request) (*http.Response, error) {
					calls.Add(1)
					if mode == "active" && r.Context().Value(key{}) != "marker" {
						t.Error("context value lost")
					}
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(contextPayload(r.URL.Path)))}, nil
				})}))
				require.NoError(t, err)
				tt.call(c, ctx)(t, want)
				if want != nil {
					require.Zero(t, calls.Load())
					require.Zero(t, store.reads.Load())
					require.Zero(t, store.writes.Load())
				}
			})
		}
	}
}

func TestAsyncContextCancelHTTP(t *testing.T) {
	for _, tt := range asyncContextCalls {
		t.Run(tt.name, func(t *testing.T) {
			entered, stopped := make(chan struct{}), make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.name == "picks" && r.URL.Path == "/bootstrap-static/" {
					_, _ = io.WriteString(w, contextBootstrap)
					return
				}
				close(entered)
				<-r.Context().Done()
				close(stopped)
			}))
			defer server.Close()
			c, err := client.NewClient(client.WithBaseURL(server.URL), freshMemoryCache(t))
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			check := tt.call(c, ctx)
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("HTTP request did not start")
			}
			cancel()
			check(t, context.Canceled)
			select {
			case <-stopped:
			case <-time.After(5 * time.Second):
				t.Fatal("handler did not observe cancellation")
			}
		})
	}
}

func TestBatchContextPropagation(t *testing.T) {
	for _, mode := range []string{"nil", "active", "canceled", "in flight"} {
		t.Run(mode, func(t *testing.T) {
			var ctx context.Context
			type key struct{}
			if mode != "nil" {
				ctx = context.WithValue(context.Background(), key{}, "marker")
			}
			cancel := func() {}
			if mode == "canceled" || mode == "in flight" {
				ctx, cancel = context.WithCancel(ctx)
			}
			defer cancel()
			if mode == "canceled" {
				cancel()
			}
			started := make(chan struct{}, 3)
			var calls atomic.Int32
			store := &contextStore{Cache: cache.NewMemoryCache()}
			c, err := client.NewClient(client.WithCache(store), client.WithHTTPClient(&http.Client{Transport: contextTransport(func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				if mode != "nil" && r.Context().Value(key{}) != "marker" {
					t.Error("batch lost context")
				}
				started <- struct{}{}
				if mode == "in flight" {
					<-r.Context().Done()
					return nil, r.Context().Err()
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"history":[]}`))}, nil
			})}))
			require.NoError(t, err)
			ch := c.Players.GetPlayerHistoriesBatch(ctx, []int{1, 2, 3})
			if mode == "in flight" {
				for range 3 {
					select {
					case <-started:
					case <-time.After(5 * time.Second):
						t.Fatal("batch request did not start")
					}
				}
				cancel()
			}
			count := 0
			for {
				select {
				case result, ok := <-ch:
					if !ok {
						if mode == "nil" || mode == "active" {
							require.Equal(t, 3, count)
						}
						if mode == "canceled" {
							require.Zero(t, calls.Load())
							require.Zero(t, store.reads.Load())
						}
						return
					}
					count++
					if mode == "nil" || mode == "active" {
						require.NoError(t, result.Err)
					} else {
						require.ErrorIs(t, result.Err, context.Canceled)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("batch did not close")
				}
			}
		})
	}
}
