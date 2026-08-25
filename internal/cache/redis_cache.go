package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisCache is an implementation of the Cache interface backed by a Redis server.
// It is ideal for distributed environments where multiple SDK instances need
// to share a common cache.
type RedisCache struct {
	client *redis.Client
	prefix string
}

// RedisOptions defines the configuration parameters for connecting to a Redis server.
type RedisOptions struct {
	// Addr is the Redis server address in "host:port" format (e.g., "localhost:6379").
	Addr string
	// Password is the authentication password for the Redis server.
	Password string
	// DB is the specific Redis database index to use.
	DB int
	// KeyPrefix is an optional string prepended to all keys to avoid collisions
	// in shared Redis environments.
	KeyPrefix string
}

// NewRedisCache initializes a new RedisCache and verifies the connection with a PING.
func NewRedisCache(opts RedisOptions) (*RedisCache, error) {
	if opts.Addr == "" {
		opts.Addr = "localhost:6379"
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:     opts.Addr,
		Password: opts.Password,
		DB:       opts.DB,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis: failed to connect to %s: %w", opts.Addr, err)
	}

	return &RedisCache{client: rdb, prefix: opts.KeyPrefix}, nil
}

// NewRedisCacheWithClient creates a RedisCache using an existing *redis.Client.
// This is useful for advanced configurations or when using Redis mocks like miniredis.
func NewRedisCacheWithClient(client *redis.Client, keyPrefix string) *RedisCache {
	return &RedisCache{client: client, prefix: keyPrefix}
}

func (r *RedisCache) prefixedKey(key string) string {
	if r.prefix == "" {
		return key
	}
	return r.prefix + ":" + key
}

// Set serializes the value to JSON and stores it in Redis with the provided TTL.
func (r *RedisCache) Set(key string, value any, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("redis cache: failed to marshal value for key %q: %w", key, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := r.client.Set(ctx, r.prefixedKey(key), data, ttl).Err(); err != nil {
		return fmt.Errorf("redis cache: failed to set key %q: %w", key, err)
	}
	return nil
}

// Get retrieves a value from Redis and unmarshals it into the destination object.
// Returns false if the key is missing, expired, or data is corrupted.
func (r *RedisCache) Get(key string, dest any) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	data, err := r.client.Get(ctx, r.prefixedKey(key)).Bytes()
	if err != nil {
		return false
	}

	return json.Unmarshal(data, dest) == nil
}

// Delete removes a specific key from Redis.
func (r *RedisCache) Delete(key string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r.client.Del(ctx, r.prefixedKey(key))
}

// Clear removes all keys from Redis that match the configured prefix.
// If no prefix is set, this operation is a no-op to prevent accidental data loss.
func (r *RedisCache) Clear() {
	if r.prefix == "" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pattern := r.prefix + ":*"
	iter := r.client.Scan(ctx, 0, pattern, 0).Iterator()
	const batchSize = 100
	batch := make([]string, 0, batchSize)
	for iter.Next(ctx) {
		batch = append(batch, iter.Val())
		if len(batch) >= batchSize {
			r.client.Del(ctx, batch...)
			batch = batch[:0]
		}
	}
	if len(batch) > 0 {
		r.client.Del(ctx, batch...)
	}
}

// Close terminates the underlying Redis connection pool.
func (r *RedisCache) Close() error {
	return r.client.Close()
}
