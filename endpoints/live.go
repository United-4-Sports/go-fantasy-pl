package endpoints

import (
	"context"
	"fmt"
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

	if err := fetchJSON(ctx, ls.client, fmt.Sprintf(eventLiveEndpoint, eventID), fetchSpec{
		fetch:    "failed to get event live data",
		read:     "failed to read event live response",
		decode:   "failed to decode event live data",
		notFound: func() error { return &EventLiveNotFoundError{EventID: eventID} },
	}, &live); err != nil {
		return nil, err
	}

	if live.Elements == nil {
		return nil, fmt.Errorf("event live data for gameweek %d is missing elements", eventID)
	}

	if err := cacheSet(ctx, ls.client, store, cacheKey, &live, eventLiveCacheTTL); err != nil {
		return nil, err
	}

	return &live, nil
}
