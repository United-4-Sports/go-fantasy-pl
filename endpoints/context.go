package endpoints

import "context"

// normalizeContext makes nil contexts behave like the legacy Background wrappers.
func normalizeContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
