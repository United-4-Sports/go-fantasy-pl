// Package endpoints implements domain-specific services for interacting with
// various FPL API endpoints.
package endpoints

import (
	"encoding/json"
	"fmt"
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
type BootstrapService struct {
	client api.Client
}

// NewBootstrapService creates a new instance of the BootstrapService.
func NewBootstrapService(client api.Client) *BootstrapService {
	return &BootstrapService{
		client: client,
	}
}

// GetTeams returns a list of all Premier League teams.
// Results are cached for 24 hours by default.
func (bs *BootstrapService) GetTeams() ([]models.Team, error) {
	const cacheKey = "teams"
	var teams []models.Team
	if sharedCache.Get(cacheKey, &teams) {
		return teams, nil
	}

	data, err := bs.fetchBootstrapData()
	if err != nil {
		return nil, fmt.Errorf("failed to get teams: %w", err)
	}
	if sharedCache == nil {
		return nil, fmt.Errorf("shared cache is not initialized")
	}

	if err := sharedCache.Set(cacheKey, data.Teams, teamsCacheTTL); err != nil {
		return nil, fmt.Errorf("failed to cache teams: %w", err)
	}
	return data.Teams, nil
}

// GetPlayers returns a list of all Premier League players (elements).
// Results are cached for 10 minutes by default.
func (bs *BootstrapService) GetPlayers() ([]models.Player, error) {
	const cacheKey = "players"
	var players []models.Player
	if sharedCache.Get(cacheKey, &players) {
		return players, nil
	}

	data, err := bs.fetchBootstrapData()
	if err != nil {
		return nil, fmt.Errorf("failed to get players: %w", err)
	}
	if sharedCache == nil {
		return nil, fmt.Errorf("shared cache is not initialized")
	}

	if err := sharedCache.Set(cacheKey, data.Elements, playersCacheTTL); err != nil {
		return nil, fmt.Errorf("failed to cache players: %w", err)
	}
	return data.Elements, nil
}

// GetGameWeeks returns a list of all gameweeks (events) for the season.
// Results are cached for 3 minutes by default.
func (bs *BootstrapService) GetGameWeeks() ([]models.GameWeek, error) {
	const cacheKey = "gameweeks"
	var gw []models.GameWeek
	if sharedCache.Get(cacheKey, &gw) {
		return gw, nil
	}

	data, err := bs.fetchBootstrapData()
	if err != nil {
		return nil, fmt.Errorf("failed to get gameweeks: %w", err)
	}

	if err := sharedCache.Set(cacheKey, data.Events, gameweeksCacheTTL); err != nil {
		return nil, fmt.Errorf("failed to cache gameweeks: %w", err)
	}
	return data.Events, nil
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
	const cacheKey = "settings"
	var settings models.GameSettings
	if sharedCache.Get(cacheKey, &settings) {
		return &settings, nil
	}

	data, err := bs.fetchBootstrapData()
	if err != nil {
		return nil, fmt.Errorf("failed to get settings: %w", err)
	}

	if err := sharedCache.Set(cacheKey, data.Settings, settingsCacheTTL); err != nil {
		return nil, fmt.Errorf("failed to cache settings: %w", err)
	}
	return &data.Settings, nil
}

func (bs *BootstrapService) fetchBootstrapData() (*Response, error) {
	resp, err := bs.client.Get(bootstrapEndpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to get bootstrap data: %w", err)
	}
	defer resp.Body.Close()

	var bootstrapResp Response
	if err := json.NewDecoder(resp.Body).Decode(&bootstrapResp); err != nil {
		return nil, fmt.Errorf("failed to decode bootstrap data: %w", err)
	}

	return &bootstrapResp, nil
}
