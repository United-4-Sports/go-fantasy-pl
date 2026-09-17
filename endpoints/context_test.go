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
	"github.com/AbdoAnss/go-fantasy-pl/internal/cache"
	"github.com/stretchr/testify/require"
)

func contextError[T any](_ T, err error) error { return err }

type contextCall struct {
	name string
	call func(*client.Client, context.Context) error
}

// Include convenience helpers: they must carry context through nested calls.
var contextCalls = []contextCall{
	{"bootstrap teams", func(c *client.Client, ctx context.Context) error {
		return contextError(c.Bootstrap.GetTeamsWithContext(ctx))
	}},
	{"bootstrap players", func(c *client.Client, ctx context.Context) error {
		return contextError(c.Bootstrap.GetPlayersWithContext(ctx))
	}},
	{"gameweeks", func(c *client.Client, ctx context.Context) error {
		return contextError(c.Bootstrap.GetGameWeeksWithContext(ctx))
	}},
	{"current gameweek", func(c *client.Client, ctx context.Context) error {
		return contextError(c.Bootstrap.GetCurrentGameWeekWithContext(ctx))
	}},
	{"next gameweek", func(c *client.Client, ctx context.Context) error {
		return contextError(c.Bootstrap.GetNextGameWeekWithContext(ctx))
	}},
	{"next model", func(c *client.Client, ctx context.Context) error {
		return contextError(c.Bootstrap.GetNextGameWeekModel(ctx))
	}},
	{"upcoming", func(c *client.Client, ctx context.Context) error {
		return contextError(c.Bootstrap.GetUpcomingGameWeeks(ctx, 1))
	}},
	{"settings", func(c *client.Client, ctx context.Context) error {
		return contextError(c.Bootstrap.GetSettingsWithContext(ctx))
	}},
	{"all players", func(c *client.Client, ctx context.Context) error {
		return contextError(c.Players.GetAllPlayersWithContext(ctx))
	}},
	{"player", func(c *client.Client, ctx context.Context) error {
		return contextError(c.Players.GetPlayerWithContext(ctx, 1))
	}},
	{"player history", func(c *client.Client, ctx context.Context) error {
		return contextError(c.Players.GetPlayerHistoryWithContext(ctx, 1))
	}},
	{"all teams", func(c *client.Client, ctx context.Context) error {
		return contextError(c.Teams.GetAllTeamsWithContext(ctx))
	}},
	{"team", func(c *client.Client, ctx context.Context) error {
		return contextError(c.Teams.GetTeamWithContext(ctx, 1))
	}},
	{"all fixtures", func(c *client.Client, ctx context.Context) error {
		return contextError(c.Fixtures.GetAllFixturesWithContext(ctx))
	}},
	{"fixture", func(c *client.Client, ctx context.Context) error {
		return contextError(c.Fixtures.GetFixtureWithContext(ctx, 1))
	}},
	{"manager", func(c *client.Client, ctx context.Context) error {
		return contextError(c.Managers.GetManagerWithContext(ctx, 1))
	}},
	{"manager history", func(c *client.Client, ctx context.Context) error {
		return contextError(c.Managers.GetManagerHistoryWithContext(ctx, 1))
	}},
	{"manager picks", func(c *client.Client, ctx context.Context) error {
		return contextError(c.Managers.GetCurrentTeamWithContext(ctx, 1))
	}},
	{"live", func(c *client.Client, ctx context.Context) error {
		return contextError(c.Live.GetEventLiveWithContext(ctx, 1))
	}},
	{"classic", func(c *client.Client, ctx context.Context) error {
		return contextError(c.Leagues.GetClassicLeagueStandingsWithContext(ctx, 1, 1))
	}},
	{"classic uncached page", func(c *client.Client, ctx context.Context) error {
		return contextError(c.Leagues.GetClassicLeagueStandingsWithContext(ctx, 1, 4))
	}},
	{"h2h matches", func(c *client.Client, ctx context.Context) error {
		return contextError(c.Leagues.GetH2HLeagueMatchesWithContext(ctx, 1, 1, 0))
	}},
	{"h2h standings", func(c *client.Client, ctx context.Context) error {
		return contextError(c.Leagues.GetH2HLeagueStandingsWithContext(ctx, 1, 1))
	}},
	{"raw", func(c *client.Client, ctx context.Context) error { return contextError(c.GetRawContext(ctx, "/raw/")) }},
}

type contextTransport func(*http.Request) (*http.Response, error)

func (f contextTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Embedding only the legacy interface intentionally prevents optional capabilities
// from bypassing the counters. Successful calls warm a real JSON cache.
type contextStore struct {
	cache.Cache
	reads, writes atomic.Int32
}

func (s *contextStore) Get(key string, dest any) bool {
	s.reads.Add(1)
	return s.Cache.Get(key, dest)
}
func (s *contextStore) Set(key string, value any, ttl time.Duration) error {
	s.writes.Add(1)
	return s.Cache.Set(key, value, ttl)
}

const contextBootstrap = `{"teams":[{"id":1}],"elements":[{"id":1}],"events":[{"id":1,"is_current":true,"is_next":true}],"game_settings":{}}`

func contextPayload(path string) string {
	path = strings.TrimPrefix(path, "/api")
	switch {
	case path == "/bootstrap-static/":
		return contextBootstrap
	case path == "/fixtures/":
		return `[{"id":1}]`
	case strings.HasPrefix(path, "/element-summary/"):
		return `{"history":[]}`
	case strings.HasPrefix(path, "/event/"):
		return `{"elements":[]}`
	default:
		return `{"id":1,"league":{"id":1},"picks":[],"current":[],"past":[],"results":[]}`
	}
}

func TestContextSurfacePropagationAndPreCanceled(t *testing.T) {
	for _, tt := range contextCalls {
		t.Run(tt.name, func(t *testing.T) {
			store := &contextStore{Cache: cache.NewMemoryCache()}
			type key struct{}
			ctx := context.WithValue(context.Background(), key{}, "marker")
			var requests atomic.Int32
			transport := contextTransport(func(r *http.Request) (*http.Response, error) {
				requests.Add(1)
				require.Equal(t, "marker", r.Context().Value(key{}))
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(contextPayload(r.URL.Path)))}, nil
			})
			c, err := client.NewClient(client.WithCache(store), client.WithHTTPClient(&http.Client{Transport: transport}))
			require.NoError(t, err)
			canceled, cancel := context.WithCancel(ctx)
			cancel()
			expired, expire := context.WithDeadline(ctx, time.Now().Add(-time.Second))
			defer expire()
			for _, warm := range []bool{false, true} {
				if warm {
					require.NoError(t, tt.call(c, ctx))
				}
				reads, writes, calls := store.reads.Load(), store.writes.Load(), requests.Load()
				require.ErrorIs(t, tt.call(c, canceled), context.Canceled)
				require.ErrorIs(t, tt.call(c, expired), context.DeadlineExceeded)
				require.Equal(t, reads, store.reads.Load(), "canceled call must not read even a warm cache")
				require.Equal(t, writes, store.writes.Load())
				require.Equal(t, calls, requests.Load())
			}
			require.Positive(t, requests.Load(), "active call must reach hermetic transport")
		})
	}
}

func TestContextSurfaceCancelHTTP(t *testing.T) {
	for _, tt := range contextCalls {
		t.Run(tt.name, func(t *testing.T) {
			entered, stopped := make(chan struct{}), make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Exercise the nested picks HTTP request, not just bootstrap.
				if tt.name == "manager picks" && r.URL.Path == "/bootstrap-static/" {
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
			done := make(chan error, 1)
			go func() { done <- tt.call(c, ctx) }()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("HTTP request did not start")
			}
			cancel()
			select {
			case err := <-done:
				require.ErrorIs(t, err, context.Canceled)
			case <-time.After(5 * time.Second):
				t.Fatal("call did not stop")
			}
			select {
			case <-stopped:
			case <-time.After(5 * time.Second):
				t.Fatal("handler did not observe cancellation")
			}
		})
	}
}

func TestContextSurfaceNil(t *testing.T) {
	for _, tt := range contextCalls {
		t.Run(tt.name, func(t *testing.T) {
			c, err := client.NewClient(freshMemoryCache(t), client.WithHTTPClient(&http.Client{Transport: contextTransport(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(contextPayload(r.URL.Path)))}, nil
			})}))
			require.NoError(t, err)
			require.NoError(t, tt.call(c, nil))
		})
	}
}
