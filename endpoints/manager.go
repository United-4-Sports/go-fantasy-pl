package endpoints

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	store := cacheFor(ms.client)
	cacheKey := fmt.Sprintf("manager_%d", id)
	var manager models.Manager
	if store.Get(cacheKey, &manager) {
		return &manager, nil
	}

	endpoint := fmt.Sprintf(managerDetailsEndpoint, id)
	resp, err := ms.client.Get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to get manager data: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, fmt.Errorf("manager with ID %d not found", id)
	default:
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if err := json.Unmarshal(body, &manager); err != nil {
		return nil, fmt.Errorf("failed to decode manager data: %w", err)
	}

	if err := ms.validateManager(&manager); err != nil {
		return nil, err
	}

	if err := store.Set(cacheKey, &manager, managerCacheTTL); err != nil {
		return nil, fmt.Errorf("failed to cache manager data: %w", err)
	}

	return &manager, nil
}

// GetCurrentTeam returns the current team selection (picks) for a manager.
func (ms *ManagerService) GetCurrentTeam(managerID int) (*models.ManagerTeam, error) {
	store := cacheFor(ms.client)
	currentGameWeekID, err := ms.bootstrapService.getCurrentGameWeek(context.Background(), store)
	if err != nil {
		return nil, fmt.Errorf("failed to get current game week: %w", err)
	}

	// Never fall back to the old manager-only key: fresh picks for an old GW
	// must not hide rollover. Old keys expire naturally. Detection is still
	// bounded by the unchanged gameweeks metadata TTL (3 minutes).
	cacheKey := fmt.Sprintf("manager_team_%d_gw%d", managerID, currentGameWeekID)
	var team models.ManagerTeam
	if store.Get(cacheKey, &team) {
		return &team, nil
	}

	endpoint := fmt.Sprintf(managerGameWeekPicksEndpoint, managerID, currentGameWeekID)
	resp, err := ms.client.Get(endpoint)
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

	if err := store.Set(cacheKey, &team, managerCacheTTL); err != nil {
		return nil, fmt.Errorf("failed to cache manager team: %w", err)
	}
	return &team, nil
}

// GetManagerHistory returns the season-by-season and gameweek-by-gameweek history for a manager.
func (ms *ManagerService) GetManagerHistory(id int) (*models.ManagerHistory, error) {
	store := cacheFor(ms.client)
	cacheKey := fmt.Sprintf("manager_history_%d", id)
	var managerHistory models.ManagerHistory
	if store.Get(cacheKey, &managerHistory) {
		return &managerHistory, nil
	}

	endpoint := fmt.Sprintf(managerHistoryEndpoint, id)
	resp, err := ms.client.Get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to get manager history data: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, fmt.Errorf("manager with ID %d not found", id)
	default:
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if err := json.Unmarshal(body, &managerHistory); err != nil {
		return nil, fmt.Errorf("failed to decode manager data: %w", err)
	}

	if err := store.Set(cacheKey, &managerHistory, managerCacheTTL); err != nil {
		return nil, fmt.Errorf("failed to cache manager history: %w", err)
	}

	return &managerHistory, nil
}
