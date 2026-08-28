package cache_test

import (
	"testing"
	"time"

	"github.com/AbdoAnss/go-fantasy-pl/internal/cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testCacheImplementation runs a suite of tests against any Cache implementation.
// It is used to validate both MemoryCache and RedisCache comply with the same contract.
func testCacheImplementation(t *testing.T, c cache.Cache) {
	t.Helper()

	t.Run("SetAndGet_string", func(t *testing.T) {
		err := c.Set("key1", "hello", time.Minute)
		require.NoError(t, err)

		var got string
		ok := c.Get("key1", &got)
		assert.True(t, ok)
		assert.Equal(t, "hello", got)
	})

	t.Run("SetAndGet_struct", func(t *testing.T) {
		type payload struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		}
		original := payload{ID: 42, Name: "FPL"}
		err := c.Set("struct_key", original, time.Minute)
		require.NoError(t, err)

		var got payload
		ok := c.Get("struct_key", &got)
		assert.True(t, ok)
		assert.Equal(t, original, got)
	})

	t.Run("SetAndGet_slice", func(t *testing.T) {
		original := []int{1, 2, 3}
		err := c.Set("slice_key", original, time.Minute)
		require.NoError(t, err)

		var got []int
		ok := c.Get("slice_key", &got)
		assert.True(t, ok)
		assert.Equal(t, original, got)
	})

	t.Run("SetAndGet_int", func(t *testing.T) {
		err := c.Set("int_key", 99, time.Minute)
		require.NoError(t, err)

		var got int
		ok := c.Get("int_key", &got)
		assert.True(t, ok)
		assert.Equal(t, 99, got)
	})

	t.Run("Get_missingKey", func(t *testing.T) {
		var got string
		ok := c.Get("nonexistent", &got)
		assert.False(t, ok)
	})

	t.Run("Delete", func(t *testing.T) {
		err := c.Set("del_key", "value", time.Minute)
		require.NoError(t, err)

		c.Delete("del_key")

		var got string
		ok := c.Get("del_key", &got)
		assert.False(t, ok, "deleted key should not be found")
	})

	t.Run("Clear", func(t *testing.T) {
		err := c.Set("clear_a", "a", time.Minute)
		require.NoError(t, err)
		err = c.Set("clear_b", "b", time.Minute)
		require.NoError(t, err)

		c.Clear()

		var a, b string
		assert.False(t, c.Get("clear_a", &a), "cleared key should not be found")
		assert.False(t, c.Get("clear_b", &b), "cleared key should not be found")
	})

	t.Run("Overwrite", func(t *testing.T) {
		err := c.Set("overwrite_key", "first", time.Minute)
		require.NoError(t, err)
		err = c.Set("overwrite_key", "second", time.Minute)
		require.NoError(t, err)

		var got string
		ok := c.Get("overwrite_key", &got)
		assert.True(t, ok)
		assert.Equal(t, "second", got)
	})
}

func TestMemoryCache(t *testing.T) {
	c := cache.NewMemoryCache()
	testCacheImplementation(t, c)
}

func TestMemoryCache_ExpiredKey(t *testing.T) {
	c := cache.NewMemoryCache()

	err := c.Set("expiring", "value", 1*time.Millisecond)
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond)

	var got string
	ok := c.Get("expiring", &got)
	assert.False(t, ok, "expired key should not be returned")
}

func TestMemoryCache_Cleanup(t *testing.T) {
	c := cache.NewMemoryCache()

	err := c.Set("a", "alive", time.Minute)
	require.NoError(t, err)
	err = c.Set("b", "dead", 1*time.Millisecond)
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond)
	c.Cleanup()

	var val string
	assert.True(t, c.Get("a", &val))
	assert.False(t, c.Get("b", &val))
}

func TestMemoryCache_StartCleanupTask(t *testing.T) {
	c := cache.NewMemoryCache()
	c.StartCleanupTask(10 * time.Millisecond)

	err := c.Set("auto_expire", "value", 5*time.Millisecond)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	var got string
	assert.False(t, c.Get("auto_expire", &got), "item should have been cleaned up")
}

func TestMemoryCache_ConcurrentAccess(t *testing.T) {
	c := cache.NewMemoryCache()
	done := make(chan struct{})

	go func() {
		for i := range 100 {
			_ = c.Set("concurrent_key", i, time.Minute)
		}
		close(done)
	}()

	for range 100 {
		var v int
		c.Get("concurrent_key", &v)
	}
	<-done
}
