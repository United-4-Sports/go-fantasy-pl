package cache

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// deleteIfStillExpired must re-read the entry under the write lock: a Set that
// lands between the caller's expired read and the cleanup must survive.
func TestMemoryCacheDeleteIfStillExpired(t *testing.T) {
	c := NewMemoryCache()

	// Expired entry: deleted.
	require.NoError(t, c.Set("gone", "old", -time.Minute))
	c.deleteIfStillExpired("gone")
	_, exists := c.items["gone"]
	require.False(t, exists, "expired entry must be deleted")

	// Fresh entry replacing it: survives the same cleanup call.
	require.NoError(t, c.Set("live", "fresh", time.Hour))
	c.deleteIfStillExpired("live")
	_, exists = c.items["live"]
	require.True(t, exists, "fresh entry must survive expiry cleanup")

	// Missing key: no panic, no resurrection.
	c.deleteIfStillExpired("missing")
}

// Deterministic interleaving: a reader observes an expired entry, a concurrent
// Set re-dates the key, and the subsequent locked cleanup must keep the fresh
// value. The fresh Set is written before deleteIfStillExpired runs, exactly
// the interleaving the write-lock re-read protects against.
func TestMemoryCacheExpiryCleanupKeepsFreshReplacement(t *testing.T) {
	c := NewMemoryCache()
	require.NoError(t, c.Set("key", "stale", -time.Minute))

	// The reader's snapshot: expired at this deadline.
	var dest string
	require.False(t, c.Get("key", &dest), "expired snapshot must miss")

	// Concurrent fresh Set before cleanup acquires the write lock.
	require.NoError(t, c.Set("key", "fresh", time.Hour))
	c.deleteIfStillExpired("key")

	require.True(t, c.Get("key", &dest), "fresh replacement must survive expiry cleanup")
	require.Equal(t, "fresh", dest)
}

// Concurrent expired readers and fresh writers under -race: expiry cleanup on
// one key must never delete fresh entries on other keys. Each writer owns a
// distinct key, so every assertion is deterministic regardless of interleaving.
func TestMemoryCacheExpiryCleanupRace(t *testing.T) {
	c := NewMemoryCache()
	const workers = 8
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if i%2 == 0 {
				// Expiry churn: write immediately-expired values and read
				// them back, running the locked cleanup on every miss.
				for range 100 {
					_ = c.Set(fmt.Sprintf("expiring_%d", i), i, -time.Nanosecond)
					var v int
					require.False(t, c.Get(fmt.Sprintf("expiring_%d", i), &v))
				}
			} else {
				// Fresh long-lived entry: must survive everyone else's cleanup.
				key := fmt.Sprintf("fresh_%d", i)
				_ = c.Set(key, i, time.Hour)
				for range 100 {
					var v int
					require.True(t, c.Get(key, &v), "fresh entry %s must survive concurrent expiry cleanup", key)
					require.Equal(t, i, v)
				}
			}
		}()
	}
	close(start)
	wg.Wait()
	for i := 1; i < workers; i += 2 {
		var v int
		require.True(t, c.Get(fmt.Sprintf("fresh_%d", i), &v))
	}
}
