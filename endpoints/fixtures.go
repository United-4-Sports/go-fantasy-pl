package endpoints

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

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
	return fs.GetAllFixturesWithContext(context.Background())
}

// GetAllFixturesWithContext returns a list of all Premier League fixtures with context.
func (fs *FixtureService) GetAllFixturesWithContext(ctx context.Context) ([]models.Fixture, error) {
	return fs.getAllFixtures(ctx, cacheFor(fs.client))
}

func (fs *FixtureService) getAllFixtures(ctx context.Context, store cache.Cache) ([]models.Fixture, error) {
	ctx = normalizeContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	const cacheKey = "fixtures"
	var fixtures []models.Fixture
	if hit, err := cacheGet(ctx, fs.client, store, cacheKey, &fixtures); err != nil {
		return nil, err
	} else if hit {
		return fixtures, nil
	}

	resp, err := fs.client.GetContext(ctx, fixturesEndpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to get fixtures: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code fetching fixtures: %d", resp.StatusCode)
	}

	// A JSON null body is not an empty fixture list; only a real array is
	// accepted (a legitimate empty season decodes as []).
	var payload *[]models.Fixture
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("failed to decode fixtures: %w", err)
	}
	if payload == nil {
		return nil, fmt.Errorf("fixtures response is missing fixture data")
	}
	fixtures = *payload

	if err := cacheSet(ctx, fs.client, store, cacheKey, fixtures, fixturesCacheTTL); err != nil {
		return nil, err
	}

	return fixtures, nil
}

// GetFixture returns a single fixture by its unique FPL ID.
func (fs *FixtureService) GetFixture(id int) (*models.Fixture, error) {
	return fs.GetFixtureWithContext(context.Background(), id)
}

// GetFixtureWithContext returns a single fixture by its unique FPL ID with context.
func (fs *FixtureService) GetFixtureWithContext(ctx context.Context, id int) (*models.Fixture, error) {
	ctx = normalizeContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store := cacheFor(fs.client)
	cacheKey := fmt.Sprintf("fixture_%d", id)
	var fixture models.Fixture
	if hit, err := cacheGet(ctx, fs.client, store, cacheKey, &fixture); err != nil {
		return nil, err
	} else if hit {
		return &fixture, nil
	}

	fixtures, err := fs.getAllFixtures(ctx, store)
	if err != nil {
		return nil, err
	}

	for _, f := range fixtures {
		if f.ID == id {
			if err := cacheSet(ctx, fs.client, store, cacheKey, &f, fixturesCacheTTL); err != nil {
				return nil, err
			}
			return &f, nil
		}
	}

	return nil, &FixtureNotFoundError{ID: id}
}
