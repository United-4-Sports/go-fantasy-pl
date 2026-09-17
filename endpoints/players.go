package endpoints

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/AbdoAnss/go-fantasy-pl/api"
	"github.com/AbdoAnss/go-fantasy-pl/internal/cache"
	"github.com/AbdoAnss/go-fantasy-pl/models"
)

const (
	playerDetailsEndpoint = "/element-summary/%d/"
)

// PlayerService provides methods for fetching player-specific data,
// including summary information and detailed historical performance.
type PlayerService struct {
	client           api.Client
	bootstrapService *BootstrapService
}

// NewPlayerService creates a new instance of the PlayerService.
func NewPlayerService(client api.Client, bootstrap *BootstrapService) *PlayerService {
	return &PlayerService{
		client:           client,
		bootstrapService: bootstrap,
	}
}

// GetAllPlayers returns a list of all players in the FPL system.
// This is a convenience wrapper around BootstrapService.GetPlayers.
func (ps *PlayerService) GetAllPlayers() ([]models.Player, error) {
	return ps.GetAllPlayersWithContext(context.Background())
}

// GetAllPlayersWithContext returns a list of all players with context.
func (ps *PlayerService) GetAllPlayersWithContext(ctx context.Context) ([]models.Player, error) {
	return ps.bootstrapService.GetPlayersWithContext(ctx)
}

// GetPlayer returns a single player by their unique FPL ID.
func (ps *PlayerService) GetPlayer(id int) (*models.Player, error) {
	return ps.GetPlayerWithContext(context.Background(), id)
}

// GetPlayerWithContext returns a single player by their unique FPL ID with context.
func (ps *PlayerService) GetPlayerWithContext(ctx context.Context, id int) (*models.Player, error) {
	players, err := ps.GetAllPlayersWithContext(ctx)
	if err != nil {
		return nil, err
	}

	for _, p := range players {
		if p.ID == id {
			return &p, nil
		}
	}
	return nil, fmt.Errorf("player with ID %d not found", id)
}

// GetPlayerHistory returns detailed historical performance data for a player,
// including past seasons and current season gameweek-by-gameweek performance.
func (ps *PlayerService) GetPlayerHistory(id int) (*models.PlayerHistory, error) {
	return ps.GetPlayerHistoryWithContext(context.Background(), id)
}

// GetPlayerHistoryWithContext returns detailed player history data with context.
func (ps *PlayerService) GetPlayerHistoryWithContext(ctx context.Context, id int) (*models.PlayerHistory, error) {
	return ps.getPlayerHistory(ctx, id, cacheFor(ps.client))
}

func (ps *PlayerService) getPlayerHistory(ctx context.Context, id int, store cache.Cache) (*models.PlayerHistory, error) {
	ctx = normalizeContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cacheKey := fmt.Sprintf("player_history_%d", id)
	var cached models.PlayerHistory
	if store.Get(cacheKey, &cached) {
		return &cached, nil
	}

	endpoint := fmt.Sprintf(playerDetailsEndpoint, id)
	resp, err := ps.client.GetContext(ctx, endpoint)
	if err != nil {
		return nil, fmt.Errorf("error fetching player history: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("player not found: %d", id)
		}
		return nil, fmt.Errorf("error fetching player history: received status code %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response: %w", err)
	}

	var history models.PlayerHistory
	if err := json.Unmarshal(bodyBytes, &history); err != nil {
		return nil, fmt.Errorf("error decoding player history: %w", err)
	}

	if history.History == nil {
		return nil, fmt.Errorf("history is nil in response for player ID %d", id)
	}

	if err := store.Set(cacheKey, &history, playersCacheTTL); err != nil {
		return nil, fmt.Errorf("failed to cache player history: %w", err)
	}
	return &history, nil
}
