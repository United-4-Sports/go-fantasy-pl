package endpoints

import (
	"encoding/json"
	"fmt"

	"github.com/AbdoAnss/go-fantasy-pl/api"
	"github.com/AbdoAnss/go-fantasy-pl/internal/cache"
	"github.com/AbdoAnss/go-fantasy-pl/models"
)

const (
	fixturesEndpoint = "/fixtures/"
)

// FixtureService provides access to Premier League fixtures and match details.
type FixtureService struct {
	client api.Client
}

// FixtureNotFoundError is returned when a specific fixture cannot be found.
type FixtureNotFoundError struct {
	ID int
}

func (e *FixtureNotFoundError) Error() string {
	return fmt.Sprintf("fixture with ID %d not found", e.ID)
}

// NewFixtureService creates a new instance of the FixtureService.
func NewFixtureService(client api.Client) *FixtureService {
	return &FixtureService{
		client: client,
	}
}

// GetAllFixtures returns a list of all Premier League fixtures for the current season.
func (fs *FixtureService) GetAllFixtures() ([]models.Fixture, error) {
	return fs.getAllFixtures(cacheFor(fs.client))
}

func (fs *FixtureService) getAllFixtures(store cache.Cache) ([]models.Fixture, error) {
	const cacheKey = "fixtures"
	var fixtures []models.Fixture
	if store.Get(cacheKey, &fixtures) {
		return fixtures, nil
	}

	resp, err := fs.client.Get(fixturesEndpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to get fixtures: %w", err)
	}
	defer resp.Body.Close()

	if err := json.NewDecoder(resp.Body).Decode(&fixtures); err != nil {
		return nil, fmt.Errorf("failed to decode fixtures: %w", err)
	}

	if err := store.Set(cacheKey, fixtures, fixturesCacheTTL); err != nil {
		return nil, fmt.Errorf("failed to cache fixtures: %w", err)
	}

	return fixtures, nil
}

// GetFixture returns a single fixture by its unique FPL ID.
func (fs *FixtureService) GetFixture(id int) (*models.Fixture, error) {
	store := cacheFor(fs.client)
	cacheKey := fmt.Sprintf("fixture_%d", id)
	var fixture models.Fixture
	if store.Get(cacheKey, &fixture) {
		return &fixture, nil
	}

	fixtures, err := fs.getAllFixtures(store)
	if err != nil {
		return nil, err
	}

	for _, f := range fixtures {
		if f.ID == id {
			if err := store.Set(cacheKey, &f, fixturesCacheTTL); err != nil {
				return nil, fmt.Errorf("failed to cache fixture %d: %w", id, err)
			}
			return &f, nil
		}
	}

	return nil, &FixtureNotFoundError{ID: id}
}
