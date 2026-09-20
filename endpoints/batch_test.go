package endpoints_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AbdoAnss/go-fantasy-pl/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// batchWorkerBound is the documented fixed worker bound for
// GetPlayerHistoriesBatch. It is repeated here (the production constant is
// unexported) so the contract is asserted from the outside.
const batchWorkerBound = 4

// historyGate is a local player-history handler that can hold requests open,
// tracking how many handler invocations are concurrently active. It never talks
// to the real FPL API.
type historyGate struct {
	t       *testing.T
	gate    chan struct{}
	active  atomic.Int32
	peak    atomic.Int32
	calls   atomic.Int32
	entered chan struct{}
	exited  chan struct{}
}

func newHistoryGate(t *testing.T, buffer int) *historyGate {
	t.Helper()
	return &historyGate{
		t:       t,
		gate:    make(chan struct{}),
		entered: make(chan struct{}, buffer),
		exited:  make(chan struct{}, buffer),
	}
}

// release unblocks every handler currently waiting on the gate.
func (g *historyGate) release() { close(g.gate) }

// raisePeak records the highest observed concurrency without holding a mutex.
func (g *historyGate) raisePeak(n int32) {
	for {
		old := g.peak.Load()
		if n <= old || g.peak.CompareAndSwap(old, n) {
			return
		}
	}
}

func (g *historyGate) serve(w http.ResponseWriter, r *http.Request) {
	g.calls.Add(1)
	g.raisePeak(g.active.Add(1))
	defer g.active.Add(-1)
	g.entered <- struct{}{}
	defer func() { g.exited <- struct{}{} }()

	select {
	case <-g.gate:
	case <-r.Context().Done():
		return
	}
	w.Header().Set("Content-Type", "application/json")
	// A cancelled or released request can legitimately fail to write; only an
	// unexpected write failure is a test problem.
	if _, err := w.Write([]byte(`{"history":[{"element":1,"round":1,"points":0}]}`)); err != nil && r.Context().Err() == nil {
		g.t.Errorf("write history response: %v", err)
	}
}

// newBatchClient starts a local server for the given handler and returns a
// client aimed at it. The rate limiter gets a generous burst so these tests
// measure worker concurrency rather than limiter throughput.
func newBatchClient(t *testing.T, handler http.HandlerFunc) *client.Client {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	c, err := client.NewClient(
		client.WithBaseURL(server.URL),
		freshMemoryCache(t),
		client.WithRateLimit(1000, time.Minute),
	)
	require.NoError(t, err)
	return c
}

// TestBatchWorkerPoolBoundsConcurrency submits more IDs than the worker bound,
// including duplicates, and asserts the pool saturates at that bound and never
// exceeds it. Handlers are gated, so no worker can finish and start the next ID
// while the gate is closed: the observed active count is the bound.
func TestBatchWorkerPoolBoundsConcurrency(t *testing.T) {
	ids := []int{101, 102, 103, 104, 105, 106, 107, 108, 109, 110, 111, 112, 101, 102}
	require.Greater(t, len(ids), batchWorkerBound, "test must submit more IDs than workers")

	want := make(map[int]int, len(ids))
	for _, id := range ids {
		want[id]++
	}

	gate := newHistoryGate(t, 2*len(ids))
	c := newBatchClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/element-summary/") {
			http.NotFound(w, r)
			return
		}
		gate.serve(w, r)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ch := c.Players.GetPlayerHistoriesBatch(ctx, ids)

	require.Eventually(t, func() bool { return gate.peak.Load() >= batchWorkerBound },
		5*time.Second, time.Millisecond, "pool did not reach the documented worker bound")
	assert.LessOrEqual(t, gate.peak.Load(), int32(batchWorkerBound), "batch exceeded the worker bound")
	assert.LessOrEqual(t, gate.calls.Load(), int32(batchWorkerBound),
		"queued IDs must not start while every worker is busy")

	gate.release()

	got := make(map[int]int, len(ids))
	total := 0
	for result := range ch {
		require.NoError(t, result.Err)
		require.NotNil(t, result.History)
		got[result.PlayerID]++
		total++
	}
	assert.Equal(t, len(ids), total, "one result per input occurrence")
	assert.Equal(t, want, got, "IDs, including duplicates, must be preserved once per occurrence")
	assert.LessOrEqual(t, gate.peak.Load(), int32(batchWorkerBound), "worker bound held for the whole run")
}

// TestBatchCancellationStopsDispatchAndExitsHandlers cancels after dispatch has
// begun and asserts the result channel closes, in-flight handlers observe
// cancellation, and no work beyond the saturated pool was dispatched.
func TestBatchCancellationStopsDispatchAndExitsHandlers(t *testing.T) {
	ids := make([]int, 40)
	for i := range ids {
		ids[i] = 1000 + i
	}

	gate := newHistoryGate(t, 2*len(ids))
	c := newBatchClient(t, func(w http.ResponseWriter, r *http.Request) {
		gate.serve(w, r)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := c.Players.GetPlayerHistoriesBatch(ctx, ids)

	// Fill every worker, then cancel mid-flight.
	for range batchWorkerBound {
		select {
		case <-gate.entered:
		case <-time.After(5 * time.Second):
			t.Fatal("batch fetches did not start")
		}
	}
	cancel()

	// Every in-flight handler must observe cancellation and exit.
	for range batchWorkerBound {
		select {
		case <-gate.exited:
		case <-time.After(5 * time.Second):
			t.Fatal("in-flight handler did not observe cancellation")
		}
	}

	delivered := 0
	for {
		select {
		case result, ok := <-ch:
			if !ok {
				assert.LessOrEqual(t, delivered, len(ids), "more results than inputs")
				assert.Equal(t, int32(batchWorkerBound), gate.calls.Load(),
					"cancellation must stop dispatch beyond the saturated pool")
				return
			}
			delivered++
			require.ErrorIs(t, result.Err, context.Canceled)
		case <-time.After(5 * time.Second):
			t.Fatal("batch did not close after cancellation")
		}
	}
}

// TestBatchEmptyInputClosesPromptly asserts an empty batch closes immediately
// and never touches the upstream API.
func TestBatchEmptyInputClosesPromptly(t *testing.T) {
	var calls atomic.Int32
	c := newBatchClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "unexpected batch fetch", http.StatusInternalServerError)
	})

	for _, ids := range [][]int{nil, {}} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		ch := c.Players.GetPlayerHistoriesBatch(ctx, ids)
		select {
		case _, ok := <-ch:
			require.False(t, ok, "empty input must close the result channel")
		case <-time.After(5 * time.Second):
			t.Fatal("empty batch did not close promptly")
		}
		cancel()
	}
	require.Zero(t, calls.Load(), "empty input must not fetch")
}

// TestBatchEarlyConsumerExitThenCancel consumes a few results, abandons the
// channel, and cancels. Work issued before cancellation is bounded by the
// results already consumed plus the buffered and in-flight worker slots, so the
// remaining IDs are never dispatched.
func TestBatchEarlyConsumerExitThenCancel(t *testing.T) {
	const consumed = 3
	ids := make([]int, 100)
	for i := range ids {
		ids[i] = 2000 + i
	}

	var calls atomic.Int32
	c := newBatchClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"history":[]}`)); err != nil && r.Context().Err() == nil {
			t.Errorf("write history response: %v", err)
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := c.Players.GetPlayerHistoriesBatch(ctx, ids)
	for range consumed {
		result, ok := <-ch
		require.True(t, ok, "batch closed before the consumer stopped reading")
		require.NoError(t, result.Err)
	}

	// Abandon the channel, then release the batch.
	cancel()

	for {
		select {
		case _, ok := <-ch:
			if !ok {
				assert.LessOrEqual(t, calls.Load(), int32(consumed+2*batchWorkerBound),
					"abandoned batch must stay bounded by its buffers and workers")
				assert.Less(t, calls.Load(), int32(len(ids)),
					"abandoned batch must not dispatch every ID")
				return
			}
		case <-time.After(5 * time.Second):
			t.Fatal("abandoned batch did not close after cancellation")
		}
	}
}
