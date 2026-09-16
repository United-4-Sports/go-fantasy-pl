package endpoints

import (
	"github.com/AbdoAnss/go-fantasy-pl/api"
	"github.com/AbdoAnss/go-fantasy-pl/internal/cache"
)

// cacheFor selects a caller-owned cache when supplied, otherwise preserving
// the legacy SetSharedCache integration. It does not change cache keys.
func cacheFor(client api.Client) cache.Cache {
	if provider, ok := client.(interface{ Cache() cache.Cache }); ok {
		if store := provider.Cache(); store != nil {
			return store
		}
	}
	return GetSharedCache()
}
