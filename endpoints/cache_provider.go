package endpoints

import (
	"context"
	"time"

	"github.com/AbdoAnss/go-fantasy-pl/api"
	"github.com/AbdoAnss/go-fantasy-pl/internal/cache"
)

// cacheFor selects an explicit cache when supplied, otherwise snapshotting the
// legacy fallback. Call once per operation and pass the store to nested helpers:
// neither reads, writes, nor bootstrap warming may switch stores mid-operation.
// Keys identify resources, not callers or upstream URLs. Different datasets must
// use separate explicit stores or Redis prefixes.
func cacheFor(client api.Client) cache.Cache {
	if provider, ok := client.(interface{ Cache() cache.Cache }); ok {
		if store := provider.Cache(); store != nil {
			return store
		}
	}
	return GetSharedCache()
}

// Legacy caches cannot expose read errors or interrupt blocking calls. Check
// cancellation on both sides without spawning unbounded fallback goroutines.
func cacheGet(ctx context.Context, client api.Client, store cache.Cache, key string, dest any) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	var hit bool
	var err error
	if contextual, ok := store.(cache.ContextCache); ok {
		hit, err = contextual.GetContext(ctx, key, dest)
	} else {
		hit = store.Get(key, dest)
	}
	if canceled := ctx.Err(); canceled != nil {
		return false, canceled
	}
	if err != nil {
		reportCacheError(client, "get", err)
		return false, ctx.Err()
	}
	return hit, nil
}

func cacheSet(ctx context.Context, client api.Client, store cache.Cache, key string, value any, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var err error
	if contextual, ok := store.(cache.ContextCache); ok {
		err = contextual.SetContext(ctx, key, value, ttl)
	} else {
		err = store.Set(key, value, ttl)
	}
	if canceled := ctx.Err(); canceled != nil {
		return canceled
	}
	if err != nil {
		reportCacheError(client, "set", err)
	}
	return ctx.Err()
}

func reportCacheError(client api.Client, operation string, err error) {
	if observer, ok := client.(interface{ ReportCacheError(string, error) }); ok {
		observer.ReportCacheError(operation, err)
	}
}
