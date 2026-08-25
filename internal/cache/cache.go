// Package cache provides caching abstractions and implementations for the FPL SDK.
// It includes a high-performance in-memory cache and support for Redis.
package cache

import (
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
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("cache: failed to marshal value for key %q: %w", key, err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.items[key] = item{
		value:      data,
		expiration: time.Now().Add(ttl),
	}
	return nil
}

// Get retrieves and deserializes the cached value into the destination object.
// Returns false if the key does not exist or has already expired.
func (c *MemoryCache) Get(key string, dest any) bool {
	c.mu.RLock()
	it, exists := c.items[key]
	c.mu.RUnlock()

	if !exists {
		return false
	}

	if time.Now().After(it.expiration) {
		go c.Delete(key)
		return false
	}

	return json.Unmarshal(it.value, dest) == nil
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
