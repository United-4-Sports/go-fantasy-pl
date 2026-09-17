package client

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/AbdoAnss/go-fantasy-pl/internal/cache"
)

const (
	cacheBackendEnv       = "FPL_CACHE_BACKEND"
	redisAddrEnv          = "REDIS_ADDR"
	redisPasswordEnv      = "REDIS_PASSWORD"
	redisDBEnv            = "REDIS_DB"
	redisKeyPrefixEnv     = "REDIS_KEY_PREFIX"
	defaultRedisAddr      = "localhost:6379"
	defaultRedisKeyPrefix = "go-fantasy-pl"
)

// Reuse memory entries across client construction, just as Redis does.
// Applications with different upstreams must supply separate explicit caches.
// The periodic cleanup task starts with the store, mirroring the endpoints
// package's shared fallback.
var defaultMemoryCache = newDefaultMemoryCache()

func newDefaultMemoryCache() *cache.MemoryCache {
	mc := cache.NewMemoryCache()
	mc.StartCleanupTask(5 * time.Minute)
	return mc
}

func configureDefaultCache() (cache.Cache, error) {
	backend := strings.ToLower(strings.TrimSpace(os.Getenv(cacheBackendEnv)))
	switch backend {
	case "memory":
		return defaultMemoryCache, nil
	case "", "auto", "redis":
		opts, err := redisOptionsFromEnv()
		if err != nil {
			return nil, err
		}
		rc, err := cache.NewRedisCache(opts)
		if err != nil {
			if backend == "redis" {
				return nil, err
			}
			return defaultMemoryCache, nil
		}
		return rc, nil
	default:
		return nil, fmt.Errorf("unsupported %s value %q", cacheBackendEnv, backend)
	}
}

func redisOptionsFromEnv() (RedisOptions, error) {
	db := 0
	if rawDB := strings.TrimSpace(os.Getenv(redisDBEnv)); rawDB != "" {
		parsedDB, err := strconv.Atoi(rawDB)
		if err != nil {
			return RedisOptions{}, fmt.Errorf("invalid %s value %q: %w", redisDBEnv, rawDB, err)
		}
		db = parsedDB
	}

	addr := strings.TrimSpace(os.Getenv(redisAddrEnv))
	if addr == "" {
		addr = defaultRedisAddr
	}

	keyPrefix := strings.TrimSpace(os.Getenv(redisKeyPrefixEnv))
	if keyPrefix == "" {
		keyPrefix = defaultRedisKeyPrefix
	}

	return RedisOptions{
		Addr:      addr,
		Password:  os.Getenv(redisPasswordEnv),
		DB:        db,
		KeyPrefix: keyPrefix,
	}, nil
}
