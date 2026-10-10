package endpoints

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/AbdoAnss/go-fantasy-pl/api"
	"github.com/AbdoAnss/go-fantasy-pl/models"
)

const (
	managerDetailsEndpoint       = "/entry/%d/"
	managerHistoryEndpoint       = "/entry/%d/history"
	managerGameWeekPicksEndpoint = "/entry/%d/event/%d/picks/"
)

// ManagerService provides methods for fetching data about FPL managers (entries).
type ManagerService struct {
	client           api.Client
	bootstrapService *BootstrapService
}

// NewManagerService creates a new instance of the ManagerService.
func NewManagerService(client api.Client, bootstrap *BootstrapService) *ManagerService {
	return &ManagerService{
		client:           client,
		bootstrapService: bootstrap,
	}
}

func (ms *ManagerService) validateManager(manager *models.Manager) error {
	if manager == nil {
		return fmt.Errorf("received nil manager data")
	}
	if manager.ID == nil {
		return fmt.Errorf("manager ID is missing")
	}
	return nil
}

// GetManager returns basic information about an FPL manager by their unique entry ID.
func (ms *ManagerService) GetManager(id int) (*models.Manager, error) {
	return ms.GetManagerWithContext(context.Background(), id)
}

// GetManagerWithContext returns basic manager information with context.
func (ms *ManagerService) GetManagerWithContext(ctx context.Context, id int) (*models.Manager, error) {
	ctx = normalizeContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store := cacheFor(ms.client)
	cacheKey := fmt.Sprintf("manager_%d", id)
	var manager models.Manager
	if hit, err := cacheGet(ctx, ms.client, store, cacheKey, &manager); err != nil {
		return nil, err
	} else if hit {
		return &manager, nil
	}

	if err := fetchJSON(ctx, ms.client, fmt.Sprintf(managerDetailsEndpoint, id), fetchSpec{
		fetch:    "failed to get manager data",
		decode:   "failed to decode manager data",
		notFound: func() error { return fmt.Errorf("manager with ID %d not found", id) },
	}, &manager); err != nil {
		return nil, err
	}

	if err := ms.validateManager(&manager); err != nil {
		return nil, err
	}

	if err := cacheSet(ctx, ms.client, store, cacheKey, &manager, managerCacheTTL); err != nil {
		return nil, err
	}

	return &manager, nil
}

// GetCurrentTeam returns the current team selection (picks) for a manager.
func (ms *ManagerService) GetCurrentTeam(managerID int) (*models.ManagerTeam, error) {
	return ms.GetCurrentTeamWithContext(context.Background(), managerID)
}

// GetCurrentTeamWithContext returns the manager's current team selection with context.
func (ms *ManagerService) GetCurrentTeamWithContext(ctx context.Context, managerID int) (*models.ManagerTeam, error) {
	ctx = normalizeContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store := cacheFor(ms.client)
	currentGameWeekID, err := ms.bootstrapService.getCurrentGameWeek(ctx, store)
	if err != nil {
		return nil, fmt.Errorf("failed to get current game week: %w", err)
	}

	// Never fall back to the old manager-only key: fresh picks for an old GW
	// must not hide rollover. Old keys expire naturally. Detection is still
	// bounded by the unchanged gameweeks metadata TTL (3 minutes).
	cacheKey := fmt.Sprintf("manager_team_%d_gw%d", managerID, currentGameWeekID)
	var team models.ManagerTeam
	if hit, err := cacheGet(ctx, ms.client, store, cacheKey, &team); err != nil {
		return nil, err
	} else if hit {
		return &team, nil
	}

	endpoint := fmt.Sprintf(managerGameWeekPicksEndpoint, managerID, currentGameWeekID)
	resp, err := ms.client.GetContext(ctx, endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to get manager team: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get manager team: status %d", resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(&team); err != nil {
		return nil, fmt.Errorf("failed to decode manager team: %w", err)
	}

	if err := cacheSet(ctx, ms.client, store, cacheKey, &team, managerCacheTTL); err != nil {
		return nil, err
	}
	return &team, nil
}

// GetManagerHistory returns the season-by-season and gameweek-by-gameweek history for a manager.
func (ms *ManagerService) GetManagerHistory(id int) (*models.ManagerHistory, error) {
	return ms.GetManagerHistoryWithContext(context.Background(), id)
}

// GetManagerHistoryWithContext returns the season and gameweek history with context.
func (ms *ManagerService) GetManagerHistoryWithContext(ctx context.Context, id int) (*models.ManagerHistory, error) {
	ctx = normalizeContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store := cacheFor(ms.client)
	cacheKey := fmt.Sprintf("manager_history_%d", id)
	var managerHistory models.ManagerHistory
	if hit, err := cacheGet(ctx, ms.client, store, cacheKey, &managerHistory); err != nil {
		return nil, err
	} else if hit {
		return &managerHistory, nil
	}

	if err := fetchJSON(ctx, ms.client, fmt.Sprintf(managerHistoryEndpoint, id), fetchSpec{
		fetch:    "failed to get manager history data",
		decode:   "failed to decode manager data",
		notFound: func() error { return fmt.Errorf("manager with ID %d not found", id) },
	}, &managerHistory); err != nil {
		return nil, err
	}

	if err := cacheSet(ctx, ms.client, store, cacheKey, &managerHistory, managerCacheTTL); err != nil {
		return nil, err
	}

	return &managerHistory, nil
}
