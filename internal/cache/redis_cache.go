package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
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
		Addr:                  opts.Addr,
		Password:              opts.Password,
		DB:                    opts.DB,
		ContextTimeoutEnabled: true,
		MaxRetries:            -1, // No automatic retries for cache operations.
		DialTimeout:           redisOperationTimeout,
		ReadTimeout:           redisOperationTimeout,
		WriteTimeout:          redisOperationTimeout,
		PoolTimeout:           redisOperationTimeout,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = rdb.Close() // This newly allocated pool cannot be returned to the caller.
		return nil, fmt.Errorf("redis: failed to connect to %s: %w", opts.Addr, redisContextError(ctx, err))
	}

	return &RedisCache{client: rdb, prefix: opts.KeyPrefix}, nil
}

const redisOperationTimeout = 5 * time.Second

// redisContextError also handles the race where the socket deadline fires before
// the context timer goroutine records Err(). Preserve both diagnostic identities.
func redisContextError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return errors.Join(ctxErr, err)
	}
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
			return errors.Join(context.DeadlineExceeded, err)
		}
	}
	return err
}

// NewRedisCacheWithClient creates a RedisCache using an existing *redis.Client.
// Options are never mutated. For deadline-bounded I/O, configure the client with
// ContextTimeoutEnabled: true, MaxRetries: -1 (disables retries), and finite
// DialTimeout, ReadTimeout, WriteTimeout and PoolTimeout (at most five seconds).
// Do not disable socket deadlines with ReadTimeout/WriteTimeout: -2; custom
// dialers/hooks must also honor contexts. Context cancellation without a deadline
// may not interrupt an in-flight socket call immediately; the five-second child
// deadline remains the ceiling. Misconfigured borrowed clients cannot guarantee
// that ceiling. Local JSON encoding/decoding is synchronous, not interruptible.
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
	return r.SetContext(context.Background(), key, value, ttl)
}

// SetContext stores JSON with a five-second child deadline; an earlier caller
// deadline wins. Already-cancelled contexts cause no serialization or Redis work.
func (r *RedisCache) SetContext(ctx context.Context, key string, value any, ttl time.Duration) error {
	ctx = cacheContext(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, redisOperationTimeout)
	defer cancel()
	data, err := json.Marshal(value)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if err != nil {
		return fmt.Errorf("redis cache: failed to marshal value for key %q: %w", key, err)
	}
	if err := redisContextError(ctx, r.client.Set(ctx, r.prefixedKey(key), data, ttl).Err()); err != nil {
		return fmt.Errorf("redis cache: failed to set key %q: %w", key, err)
	}
	return nil
}

// Get retrieves a value from Redis and unmarshals it into the destination object.
// Returns false on a miss or error; use GetContext for diagnostic errors.
func (r *RedisCache) Get(key string, dest any) bool {
	hit, _ := r.GetContext(context.Background(), key, dest)
	return hit
}

// GetContext distinguishes misses from transport/decode errors and bounds Redis
// I/O by the earlier of the caller deadline and the five-second operation ceiling.
func (r *RedisCache) GetContext(ctx context.Context, key string, dest any) (bool, error) {
	ctx = cacheContext(ctx)
	if err := ctx.Err(); err != nil {
		return false, err
	}
	ctx, cancel := context.WithTimeout(ctx, redisOperationTimeout)
	defer cancel()
	data, err := r.client.Get(ctx, r.prefixedKey(key)).Bytes()
	err = redisContextError(ctx, err)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false, fmt.Errorf("redis cache: failed to get key %q: %w", key, err)
	}
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("redis cache: failed to get key %q: %w", key, err)
	}
	err = json.Unmarshal(data, dest)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return false, ctxErr
	}
	if err != nil {
		return false, fmt.Errorf("redis cache: failed to decode key %q: %w", key, err)
	}
	return true, nil
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
