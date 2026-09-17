package endpoints

import (
	"context"
	"sync"

	"github.com/AbdoAnss/go-fantasy-pl/models"
)

// Result is a generic wrapper for a value and an error,
// used for returning results from asynchronous operations. Single-resource async
// methods deliver exactly one buffered Result (including cancellation errors),
// then close the channel. Abandoning the channel never blocks result delivery.
// A nil context is treated as context.Background(). Batch delivery is separate:
// cancellation may stop delivery of batch results.
type Result[T any] struct {
	Value T
	Err   error
}

// PlayerHistoryResult specifically wraps a player's ID and their history,
// designed for use in batch fetch operations where identifying the player is essential.
type PlayerHistoryResult struct {
	PlayerID int
	History  *models.PlayerHistory
	Err      error
}

// deliverAsync runs op once and delivers exactly one buffered Result,
// including cancellation errors, before closing the channel. Abandoning the
// channel never blocks result delivery.
func deliverAsync[T any](op func() (T, error)) <-chan Result[T] {
	ch := make(chan Result[T], 1)
	go func() {
		defer close(ch)
		value, err := op()
		ch <- Result[T]{Value: value, Err: err}
	}()
	return ch
}

// GetAllPlayersAsync fetches all players concurrently and returns a channel
// that receives a single Result containing all players or an error.
func (ps *PlayerService) GetAllPlayersAsync(ctx context.Context) <-chan Result[[]models.Player] {
	return deliverAsync(func() ([]models.Player, error) { return ps.GetAllPlayersWithContext(ctx) })
}

// GetPlayerHistoryAsync fetches the history for a single player asynchronously
// and returns a channel that receives the result.
func (ps *PlayerService) GetPlayerHistoryAsync(ctx context.Context, id int) <-chan Result[*models.PlayerHistory] {
	return deliverAsync(func() (*models.PlayerHistory, error) { return ps.GetPlayerHistoryWithContext(ctx, id) })
}

// GetPlayerHistoriesBatch fetches player histories concurrently for multiple player IDs.
// Results are sent to the returned channel as they complete.
func (ps *PlayerService) GetPlayerHistoriesBatch(ctx context.Context, ids []int) <-chan PlayerHistoryResult {
	ctx = normalizeContext(ctx)
	store := cacheFor(ps.client)
	ch := make(chan PlayerHistoryResult, len(ids))
	var wg sync.WaitGroup

	for _, id := range ids {
		wg.Add(1)
		go func(playerID int) {
			defer wg.Done()
			history, err := ps.getPlayerHistory(ctx, playerID, store)
			select {
			case ch <- PlayerHistoryResult{PlayerID: playerID, History: history, Err: err}:
			case <-ctx.Done():
			}
		}(id)
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	return ch
}

// GetAllFixturesAsync fetches all fixtures asynchronously and returns a channel
// that receives a single Result containing all fixtures or an error.
func (fs *FixtureService) GetAllFixturesAsync(ctx context.Context) <-chan Result[[]models.Fixture] {
	return deliverAsync(func() ([]models.Fixture, error) { return fs.GetAllFixturesWithContext(ctx) })
}

// GetAllTeamsAsync fetches all teams asynchronously and returns a channel
// that receives a single Result containing all teams or an error.
func (ts *TeamService) GetAllTeamsAsync(ctx context.Context) <-chan Result[[]models.Team] {
	return deliverAsync(func() ([]models.Team, error) { return ts.GetAllTeamsWithContext(ctx) })
}

// GetManagerAsync fetches a manager's profile asynchronously and returns a
// channel that receives the result.
func (ms *ManagerService) GetManagerAsync(ctx context.Context, id int) <-chan Result[*models.Manager] {
	return deliverAsync(func() (*models.Manager, error) { return ms.GetManagerWithContext(ctx, id) })
}

// GetCurrentTeamAsync fetches a manager's current team selection asynchronously
// and returns a channel that receives the result.
func (ms *ManagerService) GetCurrentTeamAsync(ctx context.Context, managerID int) <-chan Result[*models.ManagerTeam] {
	return deliverAsync(func() (*models.ManagerTeam, error) { return ms.GetCurrentTeamWithContext(ctx, managerID) })
}

// GetEventLiveAsync fetches a gameweek's live points data asynchronously and
// returns a channel that receives the result.
func (ls *LiveService) GetEventLiveAsync(ctx context.Context, eventID int) <-chan Result[*models.EventLive] {
	return deliverAsync(func() (*models.EventLive, error) { return ls.GetEventLiveWithContext(ctx, eventID) })
}

// GetManagerHistoryAsync fetches a manager's season and gameweek history
// asynchronously and returns a channel that receives the result.
func (ms *ManagerService) GetManagerHistoryAsync(ctx context.Context, id int) <-chan Result[*models.ManagerHistory] {
	return deliverAsync(func() (*models.ManagerHistory, error) { return ms.GetManagerHistoryWithContext(ctx, id) })
}
