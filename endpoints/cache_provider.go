package endpoints

import (
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
