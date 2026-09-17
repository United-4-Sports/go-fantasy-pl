package endpoints_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AbdoAnss/go-fantasy-pl/client"
	"github.com/AbdoAnss/go-fantasy-pl/endpoints"
	"github.com/stretchr/testify/require"
)

func TestCacheFailureBootstrapContinuesAfterEachSectionWriteError(t *testing.T) {
	for _, tc := range failurePaths()[:4] {
		for _, key := range tc.writes {
			t.Run(tc.name+"/"+key, func(t *testing.T) {
				h := newFailureHarness(t, tc)
				injected := errors.New("injected section write failure")
				h.store.failKey, h.store.setError = key, injected
				got, err := tc.call(context.Background(), h.client)
				h.assertClosed(t)
				require.NoError(t, err)
				assertFailureData(t, got, tc.expected)
				require.Equal(t, tc.writes, h.store.sets)
				require.Len(t, h.diagnostics, 1)
				require.Equal(t, "set", h.diagnostics[0].operation)
				require.ErrorIs(t, h.diagnostics[0].err, injected)
				for _, section := range tc.writes {
					_, stored := h.store.values[section]
					require.Equal(t, section != key, stored, "healthy section %s must still warm", section)
				}
			})
		}
	}
}

func TestCacheFailureBootstrapDoesNotWarmAbsentSections(t *testing.T) {
	tc := failurePaths()[0]
	h := newFailureHarness(t, tc)
	h.payload = `{"teams":[]}`
	got, err := tc.call(context.Background(), h.client)
	h.assertClosed(t)
	require.NoError(t, err, "a present requested section remains usable")
	assertFailureData(t, got, `[]`)
	require.Equal(t, []string{"teams"}, h.store.sets, "missing sections must not be warmed as zero values")
}

func TestCacheFailureDeadlineDuringCacheIO(t *testing.T) {
	for _, tc := range failurePaths() {
		for _, phase := range []string{"get", "set"} {
			t.Run(tc.name+"/"+phase, func(t *testing.T) {
				h := newFailureHarness(t, tc)
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
				defer cancel()
				reached := false
				hook := func(passed context.Context, key string) error {
					require.Equal(t, ctx, passed)
					if key != tc.key {
						return nil
					}
					reached = true
					<-passed.Done()
					return passed.Err()
				}
				if phase == "get" {
					h.store.onGet = hook
				} else {
					h.store.onSet = hook
				}
				_, err := tc.call(ctx, h.client)
				h.assertClosed(t)
				require.True(t, reached, "must actually expire inside cache I/O")
				require.ErrorIs(t, err, context.DeadlineExceeded)
				require.Zero(t, h.afterCancel)
				if phase == "get" {
					require.Empty(t, h.requests)
					require.Empty(t, h.store.sets)
				} else {
					require.Len(t, h.requests, 1)
					require.Equal(t, tc.key, h.store.sets[len(h.store.sets)-1])
				}
			})
		}
	}
}

func TestCacheFailureRejectsInvalidBootstrapAndFixtures(t *testing.T) {
	for _, tc := range failurePaths() {
		if !strings.HasPrefix(tc.name, "bootstrap-") && !strings.HasPrefix(tc.name, "fixtures-") {
			continue
		}
		cases := []struct {
			name, payload string
			status        int
		}{
			{"http-error-valid-json", tc.body, http.StatusServiceUnavailable},
			{"http-error-object", `{"detail":"unavailable"}`, http.StatusBadGateway},
			{"malformed", `{`, http.StatusOK},
			{"empty-body", ``, http.StatusOK},
			{"null", `null`, http.StatusOK},
			{"missing-payload", `{}`, http.StatusOK},
		}
		if strings.HasPrefix(tc.name, "bootstrap-") {
			section := map[string]string{"bootstrap-teams": "teams", "bootstrap-players": "elements", "bootstrap-gameweeks": "events", "bootstrap-settings": "game_settings"}[tc.name]
			var payload map[string]json.RawMessage
			require.NoError(t, json.Unmarshal([]byte(failureBootstrap), &payload))
			delete(payload, section)
			missing, err := json.Marshal(payload)
			require.NoError(t, err)
			cases = append(cases, struct {
				name, payload string
				status        int
			}{"missing-requested-section", string(missing), http.StatusOK})
			payload[section] = json.RawMessage(`null`)
			null, err := json.Marshal(payload)
			require.NoError(t, err)
			cases = append(cases, struct {
				name, payload string
				status        int
			}{"null-requested-section", string(null), http.StatusOK})
		}
		for _, bad := range cases {
			t.Run(tc.name+"/"+bad.name, func(t *testing.T) {
				h := newFailureHarness(t, tc)
				h.payload = bad.payload
				h.status = bad.status
				_, err := tc.call(context.Background(), h.client)
				h.assertClosed(t)
				require.Error(t, err)
				require.Empty(t, h.store.sets, "invalid response must never enter cache")
				require.Len(t, h.requests, 1)
			})
		}
	}
}

func TestCacheFailureAcceptsLegitimateEmptyArrays(t *testing.T) {
	for _, tc := range failurePaths() {
		if !strings.HasPrefix(tc.name, "bootstrap-") && tc.name != "fixtures-all" {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			h := newFailureHarness(t, tc)
			h.payload = `[]`
			expected := `[]`
			if strings.HasPrefix(tc.name, "bootstrap-") {
				h.payload = `{"teams":[],"elements":[],"events":[],"game_settings":{}}`
			}
			if tc.name == "bootstrap-settings" {
				expected = `{}`
			}
			got, err := tc.call(context.Background(), h.client)
			h.assertClosed(t)
			require.NoError(t, err)
			assertFailureData(t, got, expected)
			require.Equal(t, tc.writes, h.store.sets)
			require.Empty(t, h.diagnostics)
		})
	}
}

func TestCacheFailureClosesHTTPAndDomainErrors(t *testing.T) {
	for _, tc := range failurePaths() {
		for _, status := range []int{http.StatusNotFound, http.StatusInternalServerError} {
			name := "not-found"
			if status == http.StatusInternalServerError {
				name = "http-error"
			}
			t.Run(tc.name+"/"+name, func(t *testing.T) {
				h := newFailureHarness(t, tc)
				h.status = status
				_, err := tc.call(context.Background(), h.client)
				h.assertClosed(t)
				require.Error(t, err)
				require.Empty(t, h.store.sets)
				if status == http.StatusNotFound {
					switch tc.name {
					case "live":
						var domain *endpoints.EventLiveNotFoundError
						require.ErrorAs(t, err, &domain)
						require.Equal(t, 7, domain.EventID)
					case "classic":
						require.ErrorIs(t, err, endpoints.ErrLeagueNotFound)
					}
				}
			})
		}
		if tc.name == "manager-profile" || tc.name == "player-history" || tc.name == "live" || tc.name == "classic" {
			t.Run(tc.name+"/invalid-domain-data", func(t *testing.T) {
				h := newFailureHarness(t, tc)
				h.payload = `{}`
				_, err := tc.call(context.Background(), h.client)
				h.assertClosed(t)
				require.Error(t, err)
				require.Empty(t, h.store.sets)
			})
		}
		t.Run(tc.name+"/malformed", func(t *testing.T) {
			h := newFailureHarness(t, tc)
			h.payload = `{`
			_, err := tc.call(context.Background(), h.client)
			h.assertClosed(t)
			require.Error(t, err)
			require.Empty(t, h.store.sets)
		})
		if tc.name == "fixtures-single" {
			t.Run(tc.name+"/absent-id", func(t *testing.T) {
				h := newFailureHarness(t, tc)
				h.payload = `[]`
				_, err := tc.call(context.Background(), h.client)
				h.assertClosed(t)
				var domain *endpoints.FixtureNotFoundError
				require.ErrorAs(t, err, &domain)
				require.Equal(t, 21, domain.ID)
				require.Equal(t, []string{"fixtures"}, h.store.sets, "valid empty list may be cached, nonexistent fixture must not be")
			})
		}
	}
}

func TestCacheFailureSuccessfulResponsesSurviveSetErrors(t *testing.T) {
	for _, tc := range failurePaths() {
		t.Run(tc.name, func(t *testing.T) {
			for _, mode := range []string{"target-write", "all-writes"} {
				t.Run(mode, func(t *testing.T) {
					h := newFailureHarness(t, tc)
					injected := errors.New("injected cache write outage")
					h.store.setError = injected
					if mode == "target-write" {
						h.store.failKey = tc.key
					}
					got, err := tc.call(context.Background(), h.client)
					h.assertClosed(t)
					require.NoError(t, err)
					assertFailureData(t, got, tc.expected)
					require.Equal(t, tc.writes, h.store.sets, "all remaining writes must be attempted")
					require.Len(t, h.requests, 1, "no retries")
					wantDiagnostics := 1
					if mode == "all-writes" {
						wantDiagnostics = len(tc.writes)
					}
					require.Len(t, h.diagnostics, wantDiagnostics)
					for _, d := range h.diagnostics {
						require.Equal(t, "set", d.operation)
						require.ErrorIs(t, d.err, injected)
					}
					require.Zero(t, h.store.legacyCalls, "must use optional contextual methods")
				})
			}
		})
	}
}

func TestCacheFailureReadOutageVersusMiss(t *testing.T) {
	for _, tc := range failurePaths() {
		for _, outage := range []bool{false, true} {
			name := "miss"
			if outage {
				name = "outage"
			}
			t.Run(tc.name+"/"+name, func(t *testing.T) {
				h := newFailureHarness(t, tc)
				injected := errors.New("injected cache read outage")
				// Keep manager's gameweek metadata available so this row exercises picks,
				// not the independently covered bootstrap write path.
				if outage {
					h.store.onGet = func(_ context.Context, key string) error {
						if tc.name == "manager-picks" && key == "gameweeks" {
							return nil
						}
						return injected
					}
				}
				got, err := tc.call(context.Background(), h.client)
				h.assertClosed(t)
				require.NoError(t, err)
				assertFailureData(t, got, tc.expected)
				require.Len(t, h.requests, 1)
				if outage {
					require.NotEmpty(t, h.diagnostics, "outage must be observable, unlike a miss")
					for _, d := range h.diagnostics {
						require.Equal(t, "get", d.operation)
						require.ErrorIs(t, d.err, injected)
					}
				} else {
					require.Empty(t, h.diagnostics)
				}
				require.Zero(t, h.store.legacyCalls)
			})
		}
	}
}

func TestCacheFailureCancellationStopsWork(t *testing.T) {
	for _, tc := range failurePaths() {
		for _, phase := range []string{"before", "get", "set"} {
			t.Run(tc.name+"/"+phase, func(t *testing.T) {
				h := newFailureHarness(t, tc)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				hook := func(passed context.Context, key string) error {
					require.Same(t, ctx, passed, "cache must receive caller context")
					if key == tc.key {
						cancel()
						return passed.Err()
					}
					return nil
				}
				switch phase {
				case "before":
					cancel()
				case "get":
					h.store.onGet = hook
				case "set":
					h.store.onSet = hook
				}
				_, err := tc.call(ctx, h.client)
				h.assertClosed(t)
				require.ErrorIs(t, err, context.Canceled)
				require.Zero(t, h.afterCancel, "never launch upstream work after cancellation")
				require.Zero(t, h.store.legacyCalls)
				if phase == "set" {
					require.Len(t, h.requests, 1)
					require.NotEmpty(t, h.store.sets)
					require.Equal(t, tc.key, h.store.sets[len(h.store.sets)-1], "stop subsequent cache writes")
				} else {
					require.Empty(t, h.requests)
					require.Empty(t, h.store.sets)
					if phase == "before" {
						require.Empty(t, h.store.gets)
					}
				}
			})
		}
	}
}

// Implement both method sets explicitly: embedding MemoryCache would silently
// bypass an overridden legacy Set when endpoints select ContextCache.
type failureCache struct {
	mu                 sync.Mutex
	values             map[string]json.RawMessage
	gets, sets         []string
	legacyCalls        int
	getError, setError error
	failKey            string
	onGet, onSet       func(context.Context, string) error
}

var _ client.Cache = (*failureCache)(nil)
var _ client.ContextCache = (*failureCache)(nil)

func (s *failureCache) Get(key string, dest any) bool {
	s.mu.Lock()
	s.legacyCalls++
	s.mu.Unlock()
	hit, _ := s.GetContext(context.Background(), key, dest)
	return hit
}
func (s *failureCache) Set(key string, value any, ttl time.Duration) error {
	s.mu.Lock()
	s.legacyCalls++
	s.mu.Unlock()
	return s.SetContext(context.Background(), key, value, ttl)
}
func (s *failureCache) Delete(key string) { s.mu.Lock(); defer s.mu.Unlock(); delete(s.values, key) }
func (s *failureCache) Clear()            { s.mu.Lock(); defer s.mu.Unlock(); s.values = nil }
func (s *failureCache) GetContext(ctx context.Context, key string, dest any) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gets = append(s.gets, key)
	if s.onGet != nil {
		if err := s.onGet(ctx, key); err != nil {
			return false, err
		}
	}
	if s.getError != nil {
		return false, s.getError
	}
	data, ok := s.values[key]
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(data, dest)
}
func (s *failureCache) SetContext(ctx context.Context, key string, value any, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sets = append(s.sets, key)
	if s.onSet != nil {
		if err := s.onSet(ctx, key); err != nil {
			return err
		}
	}
	if s.setError != nil && (s.failKey == "" || s.failKey == key) {
		return s.setError
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if s.values == nil {
		s.values = make(map[string]json.RawMessage)
	}
	s.values[key] = data
	return nil
}

type failureDiagnostic struct {
	operation string
	err       error
}
type failureBody struct {
	io.Reader
	closed int
}

func (b *failureBody) Close() error { b.closed++; return nil }

type failureTransport func(*http.Request) (*http.Response, error)

func (f failureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type failureReader struct{ err error }

func (r failureReader) Read([]byte) (int, error) { return 0, r.err }

func TestCacheFailureClosesReadErrors(t *testing.T) {
	for _, tc := range failurePaths() {
		t.Run(tc.name, func(t *testing.T) {
			h := newFailureHarness(t, tc)
			h.readError = errors.New("injected response read error")
			_, err := tc.call(context.Background(), h.client)
			h.assertClosed(t)
			require.ErrorIs(t, err, h.readError)
			require.Empty(t, h.store.sets)
		})
	}
}

type failureHarness struct {
	readError   error
	client      *client.Client
	store       *failureCache
	diagnostics []failureDiagnostic
	requests    []string
	bodies      []*failureBody
	afterCancel int
	status      int
	payload     string
}

// The transport is entirely in-process and cannot contact any external host.
// It also exposes Close, unlike httptest's server-side response writer.
func newFailureHarness(t *testing.T, tc failurePath) *failureHarness {
	t.Helper()
	h := &failureHarness{store: &failureCache{}, status: http.StatusOK, payload: tc.body}
	if tc.name == "manager-picks" {
		h.store.values = map[string]json.RawMessage{"gameweeks": json.RawMessage(`[{"id":7,"is_current":true}]`)}
	}
	c, err := client.NewClient(
		client.WithCache(h.store), client.WithBaseURL("http://failure.test"),
		client.WithCacheErrorHandler(func(op string, err error) { h.diagnostics = append(h.diagnostics, failureDiagnostic{op, err}) }),
		client.WithHTTPClient(&http.Client{Transport: failureTransport(func(r *http.Request) (*http.Response, error) {
			h.requests = append(h.requests, r.URL.RequestURI())
			if r.Context().Err() != nil {
				h.afterCancel++
			}
			require.Equal(t, tc.path, r.URL.RequestURI(), "unexpected upstream request")
			var reader io.Reader = strings.NewReader(h.payload)
			if h.readError != nil {
				reader = failureReader{h.readError}
			}
			body := &failureBody{Reader: reader}
			h.bodies = append(h.bodies, body)
			return &http.Response{StatusCode: h.status, Body: body, Header: make(http.Header), Request: r}, nil
		})}),
	)
	require.NoError(t, err)
	h.client = c
	return h
}
func (h *failureHarness) assertClosed(t *testing.T) {
	t.Helper()
	for i, b := range h.bodies {
		require.Equal(t, 1, b.closed, "response %d must close exactly once", i)
	}
}

const failureBootstrap = `{"teams":[{"id":11}],"elements":[{"id":101}],"events":[{"id":7,"is_current":true},{"id":8,"is_next":true}],"game_settings":{"squad_squadsize":15}}`

type failurePath struct {
	name, path, body, expected, key string
	writes                          []string
	call                            func(context.Context, *client.Client) (any, error)
}

func assertFailureData(t *testing.T, got any, expected string) {
	t.Helper()
	require.NotNil(t, got)
	want := reflect.New(reflect.TypeOf(got))
	require.NoError(t, json.Unmarshal([]byte(expected), want.Interface()))
	require.Equal(t, want.Elem().Interface(), got, "decoded upstream data must survive cache failure")
}

func failurePaths() []failurePath {
	bw := []string{"teams", "players", "gameweeks", "settings"}
	return []failurePath{
		{"bootstrap-teams", "/bootstrap-static/", failureBootstrap, `[{"id":11}]`, "teams", bw,
			func(ctx context.Context, c *client.Client) (any, error) { return c.Bootstrap.GetTeamsWithContext(ctx) }},
		{"bootstrap-players", "/bootstrap-static/", failureBootstrap, `[{"id":101}]`, "players", bw,
			func(ctx context.Context, c *client.Client) (any, error) {
				return c.Bootstrap.GetPlayersWithContext(ctx)
			}},
		{"bootstrap-gameweeks", "/bootstrap-static/", failureBootstrap, `[{"id":7,"is_current":true},{"id":8,"is_next":true}]`, "gameweeks", bw,
			func(ctx context.Context, c *client.Client) (any, error) {
				return c.Bootstrap.GetGameWeeksWithContext(ctx)
			}},
		{"bootstrap-settings", "/bootstrap-static/", failureBootstrap, `{"squad_squadsize":15}`, "settings", bw,
			func(ctx context.Context, c *client.Client) (any, error) {
				return c.Bootstrap.GetSettingsWithContext(ctx)
			}},
		{"next-gameweek", "/bootstrap-static/", failureBootstrap, `8`, "next_gameweek", append(append([]string{}, bw...), "next_gameweek"),
			func(ctx context.Context, c *client.Client) (any, error) {
				return c.Bootstrap.GetNextGameWeekWithContext(ctx)
			}},
		{"fixtures-all", "/fixtures/", `[{"id":21}]`, `[{"id":21}]`, "fixtures", []string{"fixtures"},
			func(ctx context.Context, c *client.Client) (any, error) {
				return c.Fixtures.GetAllFixturesWithContext(ctx)
			}},
		{"fixtures-single", "/fixtures/", `[{"id":21}]`, `{"id":21}`, "fixture_21", []string{"fixtures", "fixture_21"},
			func(ctx context.Context, c *client.Client) (any, error) {
				return c.Fixtures.GetFixtureWithContext(ctx, 21)
			}},
		{"manager-profile", "/entry/123/", `{"id":123}`, `{"id":123}`, "manager_123", []string{"manager_123"},
			func(ctx context.Context, c *client.Client) (any, error) {
				return c.Managers.GetManagerWithContext(ctx, 123)
			}},
		{"manager-history", "/entry/123/history", `{"current":[{"event":7,"points":42}],"past":[],"chips":[]}`, `{"current":[{"event":7,"points":42}],"past":[],"chips":[]}`, "manager_history_123", []string{"manager_history_123"},
			func(ctx context.Context, c *client.Client) (any, error) {
				return c.Managers.GetManagerHistoryWithContext(ctx, 123)
			}},
		{"manager-picks", "/entry/123/event/7/picks/", `{"picks":[{"element":101,"position":1,"multiplier":2}]}`, `{"picks":[{"element":101,"position":1,"multiplier":2}]}`, "manager_team_123_gw7", []string{"manager_team_123_gw7"},
			func(ctx context.Context, c *client.Client) (any, error) {
				return c.Managers.GetCurrentTeamWithContext(ctx, 123)
			}},
		{"player-history", "/element-summary/101/", `{"history":[{"round":7,"total_points":9}],"history_past":[],"fixtures":[]}`, `{"history":[{"round":7,"total_points":9}],"history_past":[],"fixtures":[]}`, "player_history_101", []string{"player_history_101"},
			func(ctx context.Context, c *client.Client) (any, error) {
				return c.Players.GetPlayerHistoryWithContext(ctx, 101)
			}},
		{"live", "/event/7/live/", `{"elements":[{"id":101,"stats":{"total_points":9}}]}`, `{"elements":[{"id":101,"stats":{"total_points":9}}]}`, "event_live_7", []string{"event_live_7"},
			func(ctx context.Context, c *client.Client) (any, error) {
				return c.Live.GetEventLiveWithContext(ctx, 7)
			}},
		{"classic", "/leagues-classic/42/standings/?page_standings=1", `{"league":{"id":42},"standings":{"results":[]}}`, `{"league":{"id":42},"standings":{"results":[]}}`, "classic_league_42_page_1", []string{"classic_league_42_page_1"},
			func(ctx context.Context, c *client.Client) (any, error) {
				return c.Leagues.GetClassicLeagueStandingsWithContext(ctx, 42, 1)
			}},
	}
}
