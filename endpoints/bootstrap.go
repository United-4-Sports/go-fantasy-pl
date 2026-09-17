// Package endpoints implements domain-specific services for interacting with
// various FPL API endpoints.
package endpoints

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/AbdoAnss/go-fantasy-pl/api"
	"github.com/AbdoAnss/go-fantasy-pl/internal/cache"
	"github.com/AbdoAnss/go-fantasy-pl/models"
)

const (
	bootstrapEndpoint = "/bootstrap-static/"
)

var (
	sharedCacheMu     sync.RWMutex
	sharedCache       cache.Cache = newSharedMemoryCache()
	teamsCacheTTL                 = 24 * time.Hour   // Teams rarely change
	playersCacheTTL               = 10 * time.Minute // Players update more frequently (injuries, etc)
	fixturesCacheTTL              = 10 * time.Minute
	gameweeksCacheTTL             = 3 * time.Minute // Gameweeks status might change more often
	settingsCacheTTL              = 24 * time.Hour  // Game settings rarely change
	managerCacheTTL               = 5 * time.Minute // Managers data updates frequently
	leagueCacheTTL                = 5 * time.Minute // Leagues update frequently
)

func newSharedMemoryCache() *cache.MemoryCache {
	mc := cache.NewMemoryCache()
	mc.StartCleanupTask(5 * time.Minute)
	return mc
}

// SetSharedCache replaces the fallback for endpoint clients without an explicit
// cache. Each operation retains its selected store, including nested helpers.
// SDK clients retain their construction-time cache and are unaffected by this
// setter. Replacement does not close the old store; callers must keep it usable
// until in-flight operations finish.
func SetSharedCache(c cache.Cache) {
	if c == nil {
		c = newSharedMemoryCache()
	}
	if mc, ok := c.(*cache.MemoryCache); ok {
		mc.StartCleanupTask(5 * time.Minute)
	}
	sharedCacheMu.Lock()
	sharedCache = c
	sharedCacheMu.Unlock()
}

// GetSharedCache returns a synchronized snapshot of the legacy fallback store.
// It does not return or change the cache retained by an SDK client.
func GetSharedCache() cache.Cache {
	sharedCacheMu.RLock()
	defer sharedCacheMu.RUnlock()
	return sharedCache
}

// Response represents the full JSON response from the /bootstrap-static/ endpoint.
type Response struct {
	Teams    []models.Team       `json:"teams"`
	Elements []models.Player     `json:"elements"`
	Events   []models.GameWeek   `json:"events"`
	Settings models.GameSettings `json:"game_settings"`
}

// BootstrapService provides access to the /bootstrap-static/ endpoint,
// which contains the majority of the static data for the current FPL season.
//
// Every section (teams, players, gameweeks, settings) is cached under its
// own key with its own TTL, but a cache miss always fetches the endpoint
// once and populates all sections in a single pass — callers asking for
// two sections never trigger two downloads.
type BootstrapService struct {
	client api.Client
}

// bootstrapMu serializes bootstrap fetches so concurrent section misses
// (e.g. players and gameweeks expiring together) share one HTTP call
// instead of stampeding the endpoint.
var bootstrapMu sync.Mutex

// NewBootstrapService creates a new instance of the BootstrapService.
func NewBootstrapService(client api.Client) *BootstrapService {
	return &BootstrapService{
		client: client,
	}
}

// GetTeams returns a list of all Premier League teams.
// Results are cached for 24 hours by default.
func (bs *BootstrapService) GetTeams() ([]models.Team, error) {
	return bs.GetTeamsWithContext(context.Background())
}

// GetTeamsWithContext returns a list of all Premier League teams with context.
func (bs *BootstrapService) GetTeamsWithContext(ctx context.Context) ([]models.Team, error) {
	return bootstrapSection(ctx, bs, cacheFor(bs.client), "teams", func(r *Response) []models.Team { return r.Teams },
		func(p *bootstrapPayload) bool { return p.Teams != nil })
}

// GetPlayers returns a list of all Premier League players (elements).
// Results are cached for 10 minutes by default.
func (bs *BootstrapService) GetPlayers() ([]models.Player, error) {
	return bs.GetPlayersWithContext(context.Background())
}

// GetPlayersWithContext returns a list of all Premier League players with context.
func (bs *BootstrapService) GetPlayersWithContext(ctx context.Context) ([]models.Player, error) {
	return bootstrapSection(ctx, bs, cacheFor(bs.client), "players", func(r *Response) []models.Player { return r.Elements },
		func(p *bootstrapPayload) bool { return p.Elements != nil })
}

// GetGameWeeks returns a list of all gameweeks (events) for the season.
// Results are cached for 3 minutes by default.
func (bs *BootstrapService) GetGameWeeks() ([]models.GameWeek, error) {
	return bs.GetGameWeeksWithContext(context.Background())
}

// GetGameWeeksWithContext returns a list of all gameweeks (events) with context.
func (bs *BootstrapService) GetGameWeeksWithContext(ctx context.Context) ([]models.GameWeek, error) {
	return bs.getGameWeeks(ctx, cacheFor(bs.client))
}

func (bs *BootstrapService) getGameWeeks(ctx context.Context, store cache.Cache) ([]models.GameWeek, error) {
	return bootstrapSection(ctx, bs, store, "gameweeks", func(r *Response) []models.GameWeek { return r.Events },
		func(p *bootstrapPayload) bool { return p.Events != nil })
}

// GetCurrentGameWeek returns the ID of the current active gameweek.
// Results are backed by the cached gameweeks section.
func (bs *BootstrapService) GetCurrentGameWeek() (int, error) {
	return bs.GetCurrentGameWeekWithContext(context.Background())
}

// GetCurrentGameWeekWithContext returns the ID of the current active gameweek with context.
func (bs *BootstrapService) GetCurrentGameWeekWithContext(ctx context.Context) (int, error) {
	return bs.getCurrentGameWeek(ctx, cacheFor(bs.client))
}

func (bs *BootstrapService) getCurrentGameWeek(ctx context.Context, store cache.Cache) (int, error) {
	gameweeks, err := bs.getGameWeeks(ctx, store)
	if err != nil {
		return 0, fmt.Errorf("failed to get gameweeks: %w", err)
	}

	for _, gw := range gameweeks {
		if gw.IsCurrent {
			return gw.ID, nil
		}
	}

	return 0, fmt.Errorf("failed to find current gameweek")
}

// GetNextGameWeek returns the ID of the next upcoming gameweek (the one marked is_next).
// Results are cached for 3 minutes by default in the client's selected cache.
func (bs *BootstrapService) GetNextGameWeek() (int, error) {
	return bs.GetNextGameWeekWithContext(context.Background())
}

// GetNextGameWeekWithContext returns the ID of the next upcoming gameweek with context.
func (bs *BootstrapService) GetNextGameWeekWithContext(ctx context.Context) (int, error) {
	ctx = normalizeContext(ctx)
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	store := cacheFor(bs.client)
	const cacheKey = "next_gameweek"
	var gw int
	if hit, err := cacheGet(ctx, bs.client, store, cacheKey, &gw); err != nil {
		return 0, err
	} else if hit {
		return gw, nil
	}

	gameweeks, err := bs.getGameWeeks(ctx, store)
	if err != nil {
		return 0, fmt.Errorf("failed to get gameweeks: %w", err)
	}

	for _, gw := range gameweeks {
		if gw.IsNext {
			if err := cacheSet(ctx, bs.client, store, cacheKey, gw.ID, gameweeksCacheTTL); err != nil {
				return 0, err
			}
			return gw.ID, nil
		}
	}

	return 0, fmt.Errorf("failed to find next gameweek")
}

// GetNextGameWeekModel returns the next upcoming gameweek model (the one marked is_next) with context.
func (bs *BootstrapService) GetNextGameWeekModel(ctx context.Context) (*models.GameWeek, error) {
	gameweeks, err := bs.GetGameWeeksWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get gameweeks: %w", err)
	}

	for i := range gameweeks {
		if gameweeks[i].IsNext {
			return &gameweeks[i], nil
		}
	}

	return nil, fmt.Errorf("failed to find next gameweek")
}

// GetUpcomingGameWeeks returns the next count upcoming gameweeks, ordered chronologically.
func (bs *BootstrapService) GetUpcomingGameWeeks(ctx context.Context, count int) ([]models.GameWeek, error) {
	ctx = normalizeContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if count <= 0 {
		return []models.GameWeek{}, nil
	}

	gameweeks, err := bs.GetGameWeeksWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get gameweeks: %w", err)
	}

	var upcoming []models.GameWeek
	for _, gw := range gameweeks {
		if gw.IsUpcoming() {
			upcoming = append(upcoming, gw)
		}
	}

	sort.Slice(upcoming, func(i, j int) bool {
		return upcoming[i].ID < upcoming[j].ID
	})

	if len(upcoming) > count {
		upcoming = upcoming[:count]
	}

	return upcoming, nil
}

// GetSettings returns the game settings from the bootstrap-static endpoint.
// Results are cached for 24 hours by default.
func (bs *BootstrapService) GetSettings() (*models.GameSettings, error) {
	return bs.GetSettingsWithContext(context.Background())
}

// GetSettingsWithContext returns the game settings with context.
func (bs *BootstrapService) GetSettingsWithContext(ctx context.Context) (*models.GameSettings, error) {
	settings, err := bootstrapSection(ctx, bs, cacheFor(bs.client), "settings", func(r *Response) models.GameSettings { return r.Settings },
		func(p *bootstrapPayload) bool { return p.Settings != nil })
	if err != nil {
		return nil, err
	}
	return &settings, nil
}

// bootstrapPayload mirrors /bootstrap-static/ with pointer sections so a
// missing key is distinguishable from a legitimate empty array.
type bootstrapPayload struct {
	Teams    *[]models.Team       `json:"teams"`
	Elements *[]models.Player     `json:"elements"`
	Events   *[]models.GameWeek   `json:"events"`
	Settings *models.GameSettings `json:"game_settings"`
}

// bootstrapSection returns one cached section of the bootstrap response.
// On a miss it fetches /bootstrap-static/ at most once under a shared lock
// and populates every section's cache key with its own TTL, so callers
// asking for several sections never trigger several downloads.
func bootstrapSection[T any](ctx context.Context, bs *BootstrapService, store cache.Cache, cacheKey string, extract func(*Response) T, present func(*bootstrapPayload) bool) (T, error) {
	ctx = normalizeContext(ctx)
	var cached T
	if hit, err := cacheGet(ctx, bs.client, store, cacheKey, &cached); err != nil {
		return cached, err
	} else if hit {
		return cached, nil
	}

	// Cancellable gate: a waiter whose context ends leaves the queue without
	// disturbing the owner's fetch; the gate is process-wide by design so
	// concurrent section misses share one HTTP call.
	acquired := make(chan struct{})
	go func() {
		bootstrapMu.Lock()
		close(acquired)
	}()
	select {
	case <-acquired:
	case <-ctx.Done():
		return cached, ctx.Err()
	}
	defer bootstrapMu.Unlock()

	// Re-check under the gate: another goroutine may have populated the
	// section while we waited.
	if hit, err := cacheGet(ctx, bs.client, store, cacheKey, &cached); err != nil {
		return cached, err
	} else if hit {
		return cached, nil
	}

	resp, err := bs.client.GetContext(ctx, bootstrapEndpoint)
	if err != nil {
		return cached, fmt.Errorf("failed to get bootstrap data: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return cached, fmt.Errorf("unexpected status code fetching bootstrap data: %d", resp.StatusCode)
	}

	// Pointer decoding reveals which sections the payload actually contains:
	// missing required payloads are rejected, while legitimate empty arrays
	// (e.g. a preseason events list) remain valid.
	raw := bootstrapPayload{}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return cached, fmt.Errorf("failed to decode bootstrap data: %w", err)
	}
	if present != nil && !present(&raw) {
		return cached, fmt.Errorf("bootstrap response is missing required %s data", cacheKey)
	}

	full := Response{
		Teams:    deref(raw.Teams),
		Elements: deref(raw.Elements),
		Events:   deref(raw.Events),
		Settings: deref(raw.Settings),
	}
	value := extract(&full)

	for _, s := range []struct {
		key   string
		ttl   time.Duration
		value any
	}{
		{"teams", teamsCacheTTL, sectionValue(raw.Teams)},
		{"players", playersCacheTTL, sectionValue(raw.Elements)},
		{"gameweeks", gameweeksCacheTTL, sectionValue(raw.Events)},
		{"settings", settingsCacheTTL, sectionValue(raw.Settings)},
	} {
		value := s.value
		if value == nil {
			continue // never cache a missing section
		}
		if err := cacheSet(ctx, bs.client, store, s.key, value, s.ttl); err != nil {
			// Best-effort warming: one failed section never discards the
			// decoded requested data, and the remaining writes proceed.
			// Cancellation/deadline errors are the only fatal ones.
			return cached, err
		}
	}
	return value, nil
}

func deref[T any](v *T) T {
	if v == nil {
		var zero T
		return zero
	}
	return *v
}

// sectionValue dereferences a payload pointer for warming, returning nil when
// the section is absent so it is never cached as a zero value.
func sectionValue[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}
