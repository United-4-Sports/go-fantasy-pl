package endpoints

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AbdoAnss/go-fantasy-pl/api"
	"github.com/AbdoAnss/go-fantasy-pl/internal/cache"
	"github.com/stretchr/testify/require"
)

// These clients deliberately exercise the legacy interface without Cache().
type captureClient struct {
	payload    string
	beforeHTTP func()
}

func (c captureClient) Get(string) (*http.Response, error) {
	if c.beforeHTTP != nil {
		c.beforeHTTP()
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(c.payload))}, nil
}
func (c captureClient) GetContext(_ context.Context, path string) (*http.Response, error) {
	return c.Get(path)
}

type captureStore struct {
	cache.Cache
	onGet   func()
	getOnce sync.Once
}

func (s *captureStore) Get(key string, dest any) bool {
	if s.onGet != nil {
		s.getOnce.Do(s.onGet)
	}
	return s.Cache.Get(key, dest)
}

type changingProvider struct {
	api.Client
	first, later cache.Cache
}

func (c *changingProvider) Cache() cache.Cache {
	if c.first != nil {
		first := c.first
		c.first = nil
		return first
	}
	return c.later
}

func TestOperationCapturesCache(t *testing.T) {
	bootKeys := []string{"teams", "players", "gameweeks", "settings"}
	tests := []struct {
		name, payload string
		keys          []string
		call          func(api.Client) error
	}{
		{"bootstrap", `{"teams":[{"id":1}],"elements":[{"id":2}],"events":[],"game_settings":{}}`, bootKeys, func(c api.Client) error { _, e := NewBootstrapService(c).GetTeams(); return e }},
		{"next gameweek nested", `{"teams":[{"id":1}],"elements":[{"id":1}],"events":[{"id":7,"is_next":true}],"game_settings":{}}`, append(append([]string{}, bootKeys...), "next_gameweek"), func(c api.Client) error { _, e := NewBootstrapService(c).GetNextGameWeek(); return e }},
		{"fixture nested", `[{"id":1}]`, []string{"fixtures", "fixture_1"}, func(c api.Client) error { _, e := NewFixtureService(c).GetFixture(1); return e }},
		{"manager", `{"id":1}`, []string{"manager_1"}, func(c api.Client) error { _, e := NewManagerService(c, NewBootstrapService(c)).GetManager(1); return e }},
		{"manager history", `{}`, []string{"manager_history_1"}, func(c api.Client) error {
			_, e := NewManagerService(c, NewBootstrapService(c)).GetManagerHistory(1)
			return e
		}},
		{"manager picks nested", `{"teams":[{"id":1}],"elements":[{"id":1}],"events":[{"id":7,"is_current":true}],"game_settings":{}}`, append(append([]string{}, bootKeys...), "manager_team_1_gw7"), func(c api.Client) error {
			_, e := NewManagerService(c, NewBootstrapService(c)).GetCurrentTeam(1)
			return e
		}},
		{"player history", `{"history":[]}`, []string{"player_history_1"}, func(c api.Client) error {
			_, e := NewPlayerService(c, NewBootstrapService(c)).GetPlayerHistory(1)
			return e
		}},
		{"live", `{"elements":[]}`, []string{"event_live_1"}, func(c api.Client) error { _, e := NewLiveService(c).GetEventLive(1); return e }},
		{"classic league", `{"league":{"id":1}}`, []string{"classic_league_1_page_2"}, func(c api.Client) error { _, e := NewLeagueService(c).GetClassicLeagueStandings(1, 2); return e }},
	}
	old := GetSharedCache()
	t.Cleanup(func() { SetSharedCache(old) })
	for _, tt := range tests {
		for _, phase := range []string{"HTTP", "provider"} {
			t.Run(tt.name+"/"+phase, func(t *testing.T) {
				a := &captureStore{Cache: cache.NewMemoryCache()}
				b := cache.NewMemoryCache()
				replace := func() { SetSharedCache(b) }
				raw := captureClient{payload: tt.payload}
				SetSharedCache(a)
				var c api.Client = raw
				if phase == "HTTP" {
					raw.beforeHTTP = replace
				} else {
					c = &changingProvider{Client: raw, first: a, later: b}
				}
				require.NoError(t, tt.call(c))
				for _, key := range tt.keys {
					var value any
					require.True(t, a.Cache.Get(key, &value), "original store missing %s", key)
					require.False(t, b.Get(key, &value), "replacement store warmed %s", key)
				}
			})
		}
	}
}

func TestLegacyCacheConcurrentGetSet(t *testing.T) {
	old := GetSharedCache()
	t.Cleanup(func() { SetSharedCache(old) })
	a, b := cache.NewMemoryCache(), cache.NewMemoryCache()
	SetSharedCache(a)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for worker := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for range 1000 {
				if worker%2 == 0 {
					SetSharedCache(a)
					SetSharedCache(b)
				} else {
					selected := GetSharedCache()
					if selected != a && selected != b {
						t.Errorf("unexpected selected store %T", selected)
						return
					}
				}
			}
		}()
	}
	close(start)
	wg.Wait()
}

// Hold a cache miss while another goroutine replaces the fallback. The next
// operation must see the replacement; services do not capture at construction.
func TestLegacyOperationReplacementBarrier(t *testing.T) {
	old := GetSharedCache()
	t.Cleanup(func() { SetSharedCache(old) })
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	a := &captureStore{Cache: cache.NewMemoryCache(), onGet: func() { close(entered); <-release }}
	b := cache.NewMemoryCache()
	SetSharedCache(a)
	service := NewFixtureService(captureClient{payload: `[{"id":1}]`})
	done := make(chan error, 1)
	go func() { _, err := service.GetFixture(1); done <- err }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("operation did not reach cache")
	}
	SetSharedCache(b)
	unblock()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("operation did not finish")
	}
	var value any
	for _, key := range []string{"fixtures", "fixture_1"} {
		require.True(t, a.Cache.Get(key, &value))
		require.False(t, b.Get(key, &value))
	}
	_, err := service.GetFixture(1)
	require.NoError(t, err)
	require.True(t, b.Get("fixture_1", &value))
}

func TestPlayerHistoryBatchCapturesCache(t *testing.T) {
	old := GetSharedCache()
	t.Cleanup(func() { SetSharedCache(old) })
	b := cache.NewMemoryCache()
	a := &captureStore{Cache: cache.NewMemoryCache(), onGet: func() { SetSharedCache(b) }}
	SetSharedCache(a)
	c := captureClient{payload: `{"history":[]}`}
	service := NewPlayerService(c, NewBootstrapService(c))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	results := service.GetPlayerHistoriesBatch(ctx, []int{1, 2, 3})
	count := 0
	for result := range results {
		require.NoError(t, result.Err)
		count++
	}
	require.Equal(t, 3, count)
	for _, key := range []string{"player_history_1", "player_history_2", "player_history_3"} {
		var value any
		require.True(t, a.Cache.Get(key, &value))
		require.False(t, b.Get(key, &value))
	}
}
