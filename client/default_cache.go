package client

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/AbdoAnss/go-fantasy-pl/endpoints"
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
var defaultMemoryCache = cache.NewMemoryCache()

func configureDefaultCache() error {
	backend := strings.ToLower(strings.TrimSpace(os.Getenv(cacheBackendEnv)))

	switch backend {
	case "", "auto":
		return configureRedisWithFallback()
	case "redis":
		return configureRedisStrict()
	case "memory":
		endpoints.SetSharedCache(defaultMemoryCache)
		return nil
	default:
		return fmt.Errorf("unsupported %s value %q", cacheBackendEnv, backend)
	}
}

func configureRedisWithFallback() error {
	opts, err := redisOptionsFromEnv()
	if err != nil {
		return err
	}

	rc, err := cache.NewRedisCache(cache.RedisOptions(opts))
	if err != nil {
		endpoints.SetSharedCache(defaultMemoryCache)
		return nil
	}

	endpoints.SetSharedCache(rc)
	return nil
}

func configureRedisStrict() error {
	opts, err := redisOptionsFromEnv()
	if err != nil {
		return err
	}

	rc, err := cache.NewRedisCache(cache.RedisOptions(opts))
	if err != nil {
		return err
	}

	endpoints.SetSharedCache(rc)
	return nil
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
