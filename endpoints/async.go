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

// batchWorkers bounds how many player-history fetches GetPlayerHistoriesBatch
// runs at once. It is a fixed, documented default rather than one goroutine per
// ID. The HTTP transport's connection cap and the rate limiter are not worker
// permits: they bound total connections and sustained request rate, so the batch
// owns its own concurrency bound. Batches larger than this stay queued instead
// of flooding the upstream API.
const batchWorkers = 4

// GetPlayerHistoriesBatch fetches player histories for multiple player IDs with
// a bounded worker pool of at most batchWorkers concurrent fetches. Results are
// sent to the returned channel as they complete; completion order is unspecified.
//
// Delivery contract:
//   - Every input occurrence produces exactly one PlayerHistoryResult. IDs are
//     preserved, including duplicates, and duplicate IDs produce duplicate results.
//   - The result channel and the internal job queue are buffered to the worker
//     bound, never to len(ids), so a large batch cannot allocate one slot per ID.
//   - Every send selects on ctx.Done(). Cancellation stops dispatch and reaches
//     in-flight fetches. Partial results are allowed: an ID that was never
//     dispatched need not emit a result. Cancelling is how a caller releases a
//     batch whose channel it stops consuming; abandoning the channel without
//     cancelling leaves the workers blocked, so callers must cancel.
//   - Empty input closes the returned channel promptly without starting work.
func (ps *PlayerService) GetPlayerHistoriesBatch(ctx context.Context, ids []int) <-chan PlayerHistoryResult {
	ctx = normalizeContext(ctx)
	// Capture the store once, before any worker starts: workers and the nested
	// fetch helper must not be able to switch stores mid-operation.
	store := cacheFor(ps.client)

	workers := min(batchWorkers, len(ids))
	ch := make(chan PlayerHistoryResult, workers)
	if workers == 0 {
		close(ch)
		return ch
	}

	jobs := make(chan int, workers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case id, ok := <-jobs:
					if !ok {
						return
					}
					history, err := ps.getPlayerHistory(ctx, id, store)
					select {
					case ch <- PlayerHistoryResult{PlayerID: id, History: history, Err: err}:
					case <-ctx.Done():
						return
					}
				}
			}
		}()
	}

	// Dispatch keeps at most one buffered job per worker outstanding; it stops as
	// soon as the context is done, so unstarted IDs are simply dropped.
	go func() {
		defer close(jobs)
		for _, id := range ids {
			select {
			case jobs <- id:
			case <-ctx.Done():
				return
			}
		}
	}()

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
