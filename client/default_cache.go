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

// configureDefaultCache resolves the cache for clients without an explicit
// cache option. The boolean reports whether the SDK created (and therefore
// owns) the returned store: true only for a pool it dialed itself.
func configureDefaultCache() (cache.Cache, bool, error) {
	backend := strings.ToLower(strings.TrimSpace(os.Getenv(cacheBackendEnv)))
	switch backend {
	case "memory":
		return defaultMemoryCache, false, nil
	case "", "auto", "redis":
		opts, err := redisOptionsFromEnv()
		if err != nil {
			return nil, false, err
		}
		rc, err := cache.NewRedisCache(opts)
		if err != nil {
			if backend == "redis" {
				return nil, false, err
			}
			return defaultMemoryCache, false, nil
		}
		return rc, true, nil
	default:
		return nil, false, fmt.Errorf("unsupported %s value %q", cacheBackendEnv, backend)
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
