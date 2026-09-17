package client

import "github.com/AbdoAnss/go-fantasy-pl/internal/cache"

// ContextCache is an optional capability for cancellable cache reads and writes.
// Cache remains unchanged; blocking legacy caches cannot be forcibly cancelled.
type ContextCache = cache.ContextCache

// WithCacheErrorHandler observes best-effort cache failures. The handler runs
// inline, must be concurrency-safe and nonblocking, and may be nil to disable
// diagnostics. Operation is "get" or "set". Errors come from the configured
// cache; no payloads or credentials are added by the SDK. Custom caches must
// avoid including secrets in their errors. Legacy Get cannot report errors.
func WithCacheErrorHandler(handler func(operation string, err error)) Option {
	return func(c *Client) { c.cacheErrorHandler = handler }
}

// ReportCacheError reports an ordinary cache failure without discarding valid
// upstream data. It implements the optional endpoint diagnostics capability.
func (c *Client) ReportCacheError(operation string, err error) {
	if c.cacheErrorHandler != nil {
		c.cacheErrorHandler(operation, err)
	}
}
