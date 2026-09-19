package endpoints

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/AbdoAnss/go-fantasy-pl/internal/cache"
	"github.com/stretchr/testify/require"
)

// This wrapper deliberately exposes only the original cache interface.
type legacyPolicyStore struct {
	cache.Cache
	get func() bool
}

func (s legacyPolicyStore) Get(_ string, _ any) bool { return s.get() }

type contextPolicyStore struct {
	cache.Cache
	err    error
	cancel context.CancelFunc
}

func (s contextPolicyStore) GetContext(context.Context, string, any) (bool, error) {
	if s.cancel != nil {
		s.cancel()
	}
	return false, s.err
}
func (s contextPolicyStore) SetContext(context.Context, string, any, time.Duration) error {
	if s.cancel != nil {
		s.cancel()
	}
	return s.err
}

func TestCachePolicyCancellation(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		var store cache.Cache = contextPolicyStore{Cache: cache.NewMemoryCache(), cancel: cancel}
		if legacy {
			store = legacyPolicyStore{Cache: cache.NewMemoryCache(), get: func() bool { cancel(); return true }}
		}
		var value any
		hit, err := cacheGet(ctx, nil, store, "key", &value)
		require.False(t, hit)
		require.ErrorIs(t, err, context.Canceled)
		cancel()
	}
}

func TestCachePolicyOrdinaryErrorsAreBestEffort(t *testing.T) {
	store := contextPolicyStore{Cache: cache.NewMemoryCache(), err: errors.New("unavailable")}
	var value any
	hit, err := cacheGet(context.Background(), nil, store, "key", &value)
	require.False(t, hit)
	require.NoError(t, err)
	require.NoError(t, cacheSet(context.Background(), nil, store, "key", 1, time.Minute))
}
