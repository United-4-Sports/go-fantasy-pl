package endpoints

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/AbdoAnss/go-fantasy-pl/api"
	"github.com/AbdoAnss/go-fantasy-pl/models"
)

const (
	classicLeagueEndpoint = "/leagues-classic/%d/standings/?page_standings=%d"
	h2hLeagueMatchesPath  = "/leagues-h2h-matches/league/%d/"
	h2hLeagueStandings    = "/leagues-h2h/%d/standings/"
	maxPageCache          = 3 // Only cache first 3 pages
)

// ErrLeagueNotFound is wrapped into every league error caused by an HTTP 404,
// so callers can match it with errors.Is regardless of league type.
var ErrLeagueNotFound = errors.New("league not found")

// ErrInvalidH2HQuery is returned when the FPL API rejects an H2H matches
// request with HTTP 400. In practice this happens for an event value outside
// the valid gameweek range (e.g. event=999).
type ErrInvalidH2HQuery struct {
	LeagueID int
	Page     int
	Event    int
	Detail   string
}

func (e *ErrInvalidH2HQuery) Error() string {
	msg := fmt.Sprintf("invalid query for H2H league %d (page=%d, event=%d): the FPL API rejected the request with 400",
		e.LeagueID, e.Page, e.Event)
	if e.Detail != "" {
		msg += ": " + e.Detail
	}
	return msg
}

// LeagueService provides methods for fetching league standings and details,
// supporting both classic and head-to-head (H2H) leagues.
type LeagueService struct {
	client api.Client
}

// NewLeagueService creates a new instance of the LeagueService.
func NewLeagueService(client api.Client) *LeagueService {
	return &LeagueService{
		client: client,
	}
}

// GetClassicLeagueStandings returns the standings for a classic league by its unique ID.
// The page parameter allows for paginated access to large leagues (50 entries per page).
func (ls *LeagueService) GetClassicLeagueStandings(id, page int) (*models.ClassicLeague, error) {
	return ls.GetClassicLeagueStandingsWithContext(context.Background(), id, page)
}

// GetClassicLeagueStandingsWithContext returns classic league standings with context.
func (ls *LeagueService) GetClassicLeagueStandingsWithContext(ctx context.Context, id, page int) (*models.ClassicLeague, error) {
	ctx = normalizeContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store := cacheFor(ls.client)
	// Only cache first few pages to prevent memory bloat
	useCache := page <= maxPageCache

	if useCache {
		cacheKey := fmt.Sprintf("classic_league_%d_page_%d", id, page)
		var league models.ClassicLeague
		if store.Get(cacheKey, &league) {
			return &league, nil
		}
	}

	endpoint := fmt.Sprintf(classicLeagueEndpoint, id, page)
	resp, err := ls.client.GetContext(ctx, endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to get league standings: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, fmt.Errorf("league with ID %d not found: %w", id, ErrLeagueNotFound)
	default:
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var league models.ClassicLeague
	if err := json.Unmarshal(body, &league); err != nil {
		return nil, fmt.Errorf("failed to decode league data: %w", err)
	}

	if err := ls.validateLeague(&league); err != nil {
		return nil, err
	}

	if useCache {
		cacheKey := fmt.Sprintf("classic_league_%d_page_%d", id, page)
		if err := store.Set(cacheKey, &league, leagueCacheTTL); err != nil {
			return nil, fmt.Errorf("failed to cache league standings: %w", err)
		}
	}

	return &league, nil
}

func (ls *LeagueService) validateLeague(league *models.ClassicLeague) error {
	if league == nil {
		return fmt.Errorf("received nil league data")
	}
	if league.League.ID == 0 {
		return fmt.Errorf("invalid league ID")
	}
	return nil
}

// GetTotalPages calculates the total number of pages in a classic league.
func (ls *LeagueService) GetTotalPages(league *models.ClassicLeague) int {
	if league == nil {
		return 0
	}

	// MaxEntries, when the league declares it, determines pagination even if
	// the fetched page has no rows yet (e.g. pre-season).
	totalEntries := len(league.Standings.Results)
	if league.League.GetMaxEntries() > 0 {
		totalEntries = league.League.GetMaxEntries()
	}
	if totalEntries == 0 {
		return 0
	}

	entriesPerPage := 50 // FPL default
	return (totalEntries + entriesPerPage - 1) / entriesPerPage
}

// GetH2HLeagueMatches returns paginated H2H league match results.
//
// Semantics (mirroring the live FPL API):
//   - Omitting event (0) returns a paginated mixed feed of matches spanning
//     multiple gameweeks.
//   - Setting event filters matches to that gameweek (or knockout round when
//     the value falls in the league's knockout range); has_next reflects the
//     filtered query, so it can differ from the mixed feed.
//   - Knockout matches have is_knockout=true, a non-nil winner, and a
//     knockout_name such as "Round 1".
//   - An event outside the valid gameweek range (e.g. 999) makes the API
//     respond with HTTP 400, surfaced as *ErrInvalidH2HQuery.
//
// Page starts from 1 and event is optional (set 0 to omit it).
func (ls *LeagueService) GetH2HLeagueMatches(id, page, event int) (*models.H2HLeagueMatchesPage, error) {
	return ls.GetH2HLeagueMatchesWithContext(context.Background(), id, page, event)
}

// GetH2HLeagueMatchesWithContext returns paginated H2H matches with context.
func (ls *LeagueService) GetH2HLeagueMatchesWithContext(ctx context.Context, id, page, event int) (*models.H2HLeagueMatchesPage, error) {
	ctx = normalizeContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if id <= 0 {
		return nil, fmt.Errorf("league ID must be positive")
	}
	if page <= 0 {
		return nil, fmt.Errorf("page must be positive")
	}
	if event < 0 {
		return nil, fmt.Errorf("event cannot be negative")
	}

	params := url.Values{}
	params.Set("page", fmt.Sprintf("%d", page))
	if event > 0 {
		params.Set("event", fmt.Sprintf("%d", event))
	}

	endpoint := fmt.Sprintf("%s?%s", fmt.Sprintf(h2hLeagueMatchesPath, id), params.Encode())
	resp, err := ls.client.GetContext(ctx, endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to get H2H league matches: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, fmt.Errorf("league with ID %d not found: %w", id, ErrLeagueNotFound)
	case http.StatusBadRequest:
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			body = nil
		}
		return nil, &ErrInvalidH2HQuery{
			LeagueID: id,
			Page:     page,
			Event:    event,
			Detail:   parseAPIErrorDetail(body),
		}
	default:
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var matches models.H2HLeagueMatchesPage
	if err := json.Unmarshal(body, &matches); err != nil {
		return nil, fmt.Errorf("failed to decode H2H matches data: %w", err)
	}

	return &matches, nil
}

// parseAPIErrorDetail extracts a human-readable message from an error
// response body such as {"detail": "Not found."}.
func parseAPIErrorDetail(body []byte) string {
	var payload struct {
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	return payload.Detail
}

// GetH2HLeagueStandings returns paginated H2H league standings.
func (ls *LeagueService) GetH2HLeagueStandings(id, page int) (*models.H2HLeagueStandings, error) {
	return ls.GetH2HLeagueStandingsWithContext(context.Background(), id, page)
}

// GetH2HLeagueStandingsWithContext returns paginated H2H standings with context.
func (ls *LeagueService) GetH2HLeagueStandingsWithContext(ctx context.Context, id, page int) (*models.H2HLeagueStandings, error) {
	ctx = normalizeContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if id <= 0 {
		return nil, fmt.Errorf("league ID must be positive")
	}
	if page <= 0 {
		return nil, fmt.Errorf("page must be positive")
	}

	endpoint := fmt.Sprintf("%s?page_standings=%d", fmt.Sprintf(h2hLeagueStandings, id), page)
	resp, err := ls.client.GetContext(ctx, endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to get H2H league standings: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, fmt.Errorf("league with ID %d not found: %w", id, ErrLeagueNotFound)
	default:
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var standings models.H2HLeagueStandings
	if err := json.Unmarshal(body, &standings); err != nil {
		return nil, fmt.Errorf("failed to decode H2H league standings data: %w", err)
	}

	if standings.League.ID == 0 {
		return nil, fmt.Errorf("invalid league ID")
	}

	return &standings, nil
}
