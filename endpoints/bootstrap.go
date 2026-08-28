// Package endpoints implements domain-specific services for interacting with
// various FPL API endpoints.
package endpoints

import (
	"encoding/json"
	"fmt"
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

// SetSharedCache replaces the cache used by all endpoint services.
// This is used globally by the SDK to ensure consistent caching across services.
func SetSharedCache(c cache.Cache) {
	if c == nil {
		c = newSharedMemoryCache()
	}
	if mc, ok := c.(*cache.MemoryCache); ok {
		mc.StartCleanupTask(5 * time.Minute)
	}
	sharedCache = c
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
	return bootstrapSection(bs, "teams", func(r *Response) []models.Team { return r.Teams })
}

// GetPlayers returns a list of all Premier League players (elements).
// Results are cached for 10 minutes by default.
func (bs *BootstrapService) GetPlayers() ([]models.Player, error) {
	return bootstrapSection(bs, "players", func(r *Response) []models.Player { return r.Elements })
}

// GetGameWeeks returns a list of all gameweeks (events) for the season.
// Results are cached for 3 minutes by default.
func (bs *BootstrapService) GetGameWeeks() ([]models.GameWeek, error) {
	return bootstrapSection(bs, "gameweeks", func(r *Response) []models.GameWeek { return r.Events })
}

// GetCurrentGameWeek returns the ID of the current active gameweek.
// Results are cached for 3 minutes by default.
func (bs *BootstrapService) GetCurrentGameWeek() (int, error) {
	const cacheKey = "current_gameweek"
	var gw int
	if sharedCache.Get(cacheKey, &gw) {
		return gw, nil
	}

	gameweeks, err := bs.GetGameWeeks()
	if err != nil {
		return 0, fmt.Errorf("failed to get gameweeks: %w", err)
	}

	for _, gw := range gameweeks {
		if gw.IsCurrent {
			if err := sharedCache.Set(cacheKey, gw.ID, gameweeksCacheTTL); err != nil {
				return 0, fmt.Errorf("failed to cache current gameweek: %w", err)
			}
			return gw.ID, nil
		}
	}

	return 0, fmt.Errorf("failed to find current gameweek")
}

// GetSettings returns the game settings from the bootstrap-static endpoint.
// Results are cached for 24 hours by default.
func (bs *BootstrapService) GetSettings() (*models.GameSettings, error) {
	settings, err := bootstrapSection(bs, "settings", func(r *Response) models.GameSettings { return r.Settings })
	if err != nil {
		return nil, err
	}
	return &settings, nil
}

// bootstrapSection returns one cached section of the bootstrap response.
// On a miss it fetches /bootstrap-static/ at most once under a shared lock
// and populates every section's cache key with its own TTL, so callers
// asking for several sections never trigger several downloads.
func bootstrapSection[T any](bs *BootstrapService, cacheKey string, extract func(*Response) T) (T, error) {
	var cached T
	if sharedCache.Get(cacheKey, &cached) {
		return cached, nil
	}

	bootstrapMu.Lock()
	defer bootstrapMu.Unlock()

	// Re-check under the lock: another goroutine may have populated the
	// section while we waited.
	if sharedCache.Get(cacheKey, &cached) {
		return cached, nil
	}

	resp, err := bs.client.Get(bootstrapEndpoint)
	if err != nil {
		return cached, fmt.Errorf("failed to get bootstrap data: %w", err)
	}
	defer resp.Body.Close()

	var full Response
	if err := json.NewDecoder(resp.Body).Decode(&full); err != nil {
		return cached, fmt.Errorf("failed to decode bootstrap data: %w", err)
	}

	for _, s := range []struct {
		key   string
		ttl   time.Duration
		value any
	}{
		{"teams", teamsCacheTTL, full.Teams},
		{"players", playersCacheTTL, full.Elements},
		{"gameweeks", gameweeksCacheTTL, full.Events},
		{"settings", settingsCacheTTL, full.Settings},
	} {
		if err := sharedCache.Set(s.key, s.value, s.ttl); err != nil {
			return cached, fmt.Errorf("failed to cache %s: %w", s.key, err)
		}
	}
	return extract(&full), nil
}
