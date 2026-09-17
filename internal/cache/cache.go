// Package cache provides caching abstractions and implementations for the FPL SDK.
// It includes a high-performance in-memory cache and support for Redis.
package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// Cache defines the standard interface for all cache implementations used by the SDK.
// Implementations must be safe for concurrent use across multiple goroutines.
type Cache interface {
	// Get retrieves a value from the cache by key and unmarshals it into dest.
	// Returns true if the key exists and has not expired, false otherwise.
	Get(key string, dest any) bool
	// Set serializes a value and stores it in the cache with the specified TTL.
	Set(key string, value any, ttl time.Duration) error
	// Delete removes a specific key from the cache.
	Delete(key string)
	// Clear removes all keys from the cache.
	Clear()
}

// ContextCache is an optional capability; Cache remains unchanged for legacy
// implementations. A successful decode returns (true, nil), a missing or expired
// key returns (false, nil), and read/decode failures return (false, error).
// Context errors preserve errors.Is identity. Implementations must be concurrency
// safe. Built-in caches accept nil contexts as context.Background().
// Legacy boolean Get cannot report read or decode errors.
// Callers falling back to Cache must check ctx before calling; blocking legacy
// methods cannot be forcibly cancelled and must not be hidden in goroutines.
type ContextCache interface {
	GetContext(ctx context.Context, key string, dest any) (bool, error)
	SetContext(ctx context.Context, key string, value any, ttl time.Duration) error
}

func cacheContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

type item struct {
	value      []byte
	expiration time.Time
}

// MemoryCache is an in-memory implementation of the Cache interface.
// It uses a map with a read-write mutex for thread-safe access.
type MemoryCache struct {
	items       map[string]item
	mu          sync.RWMutex
	cleanupOnce sync.Once
}

// NewMemoryCache initializes and returns a new MemoryCache.
func NewMemoryCache() *MemoryCache {
	return &MemoryCache{
		items: make(map[string]item),
	}
}

// Set serializes the provided value to JSON and stores it with the given TTL.
func (c *MemoryCache) Set(key string, value any, ttl time.Duration) error {
	return c.SetContext(context.Background(), key, value, ttl)
}

// SetContext stores a value unless ctx is cancelled. JSON serialization and mutex
// acquisition are synchronous; cancellation is checked before and after them.
func (c *MemoryCache) SetContext(ctx context.Context, key string, value any, ttl time.Duration) error {
	ctx = cacheContext(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("cache: failed to marshal value for key %q: %w", key, err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}
	c.items[key] = item{
		value:      data,
		expiration: time.Now().Add(ttl),
	}
	return nil
}

// Get retrieves and deserializes the cached value into the destination object.
// Returns false on a miss or error; use GetContext for diagnostic errors.
func (c *MemoryCache) Get(key string, dest any) bool {
	hit, _ := c.GetContext(context.Background(), key, dest)
	return hit
}

// GetContext distinguishes missing/expired entries from decode failures.
// Like SetContext, local locking and JSON decoding are synchronous.
func (c *MemoryCache) GetContext(ctx context.Context, key string, dest any) (bool, error) {
	ctx = cacheContext(ctx)
	if err := ctx.Err(); err != nil {
		return false, err
	}
	c.mu.RLock()
	it, exists := c.items[key]
	c.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if !exists {
		return false, nil
	}

	if time.Now().After(it.expiration) {
		c.deleteIfStillExpired(key)
		return false, ctx.Err()
	}

	err := json.Unmarshal(it.value, dest)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return false, ctxErr
	}
	if err != nil {
		return false, fmt.Errorf("cache: failed to decode key %q: %w", key, err)
	}
	return true, nil
}

// deleteIfStillExpired deletes key only if it is still expired, while holding
// the write lock. Re-reading under the lock prevents deleting a concurrent
// fresh Set: between the caller's expired read and acquiring the lock, Set may
// have re-dated the entry, and that fresh entry must win. The check runs
// synchronously (no goroutine per miss).
func (c *MemoryCache) deleteIfStillExpired(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	it, exists := c.items[key]
	if !exists || !time.Now().After(it.expiration) {
		return
	}
	delete(c.items, key)
}

// Delete removes a key from the cache.
func (c *MemoryCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, key)
}

// Clear removes all items from the cache.
func (c *MemoryCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]item)
}

// Cleanup removes all expired items from the cache to reclaim memory.
func (c *MemoryCache) Cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for k, v := range c.items {
		if now.After(v.expiration) {
			delete(c.items, k)
		}
	}
}

// StartCleanupTask launches a background goroutine that periodically calls Cleanup.
func (c *MemoryCache) StartCleanupTask(interval time.Duration) {
	c.cleanupOnce.Do(func() {
		ticker := time.NewTicker(interval)
		go func() {
			for range ticker.C {
				c.Cleanup()
			}
		}()
	})
}
