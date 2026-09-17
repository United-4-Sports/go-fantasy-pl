package endpoints

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/AbdoAnss/go-fantasy-pl/api"
	"github.com/AbdoAnss/go-fantasy-pl/models"
)

const (
	eventLiveEndpoint = "/event/%d/live/"
	// eventLiveCacheTTL is deliberately short: during live matches the
	// payload changes every few seconds upstream, and the FPL CDN itself
	// serves cached copies (edge-control max-age=300), so caching longer
	// adds staleness on top of an already "live-ish" source.
	eventLiveCacheTTL = 30 * time.Second
)

// EventLiveNotFoundError is returned when a gameweek has no live data
// (typically an out-of-range gameweek ID).
type EventLiveNotFoundError struct {
	EventID int
}

func (e *EventLiveNotFoundError) Error() string {
	return fmt.Sprintf("live data for gameweek %d not found", e.EventID)
}

// LiveService provides access to gameweek live points data.
type LiveService struct {
	client api.Client
}

// NewLiveService creates a new instance of the LiveService.
func NewLiveService(client api.Client) *LiveService {
	return &LiveService{
		client: client,
	}
}

// GetEventLive returns the live points data for every player in a gameweek.
//
// The payload is gameweek-scoped and manager-agnostic: one request covers
// all players, so a manager's live total is computed by joining these stats
// with their picks (via Managers.GetCurrentTeam) and summing
// stats.TotalPoints * multiplier.
//
// Note that bonus points are provisional while fixtures are in progress,
// and upstream CDN caching means data can lag reality by a few minutes.
func (ls *LiveService) GetEventLive(eventID int) (*models.EventLive, error) {
	return ls.GetEventLiveWithContext(context.Background(), eventID)
}

// GetEventLiveWithContext returns the live points data for a gameweek with context.
func (ls *LiveService) GetEventLiveWithContext(ctx context.Context, eventID int) (*models.EventLive, error) {
	ctx = normalizeContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store := cacheFor(ls.client)
	cacheKey := fmt.Sprintf("event_live_%d", eventID)
	var live models.EventLive
	if hit, err := cacheGet(ctx, ls.client, store, cacheKey, &live); err != nil {
		return nil, err
	} else if hit {
		return &live, nil
	}

	endpoint := fmt.Sprintf(eventLiveEndpoint, eventID)
	resp, err := ls.client.GetContext(ctx, endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to get event live data: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, &EventLiveNotFoundError{EventID: eventID}
	default:
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read event live response: %w", err)
	}

	if err := json.Unmarshal(body, &live); err != nil {
		return nil, fmt.Errorf("failed to decode event live data: %w", err)
	}

	if live.Elements == nil {
		return nil, fmt.Errorf("event live data for gameweek %d is missing elements", eventID)
	}

	if err := cacheSet(ctx, ls.client, store, cacheKey, &live, eventLiveCacheTTL); err != nil {
		return nil, err
	}

	return &live, nil
}
