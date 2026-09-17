package cache

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

var _ ContextCache = (*MemoryCache)(nil)
var _ ContextCache = (*RedisCache)(nil)

// This deliberately has only the original method set.
type legacyCache struct{}

func (legacyCache) Get(string, any) bool                 { return false }
func (legacyCache) Set(string, any, time.Duration) error { return nil }
func (legacyCache) Delete(string)                        {}
func (legacyCache) Clear()                               {}

var _ Cache = legacyCache{}

type forbiddenJSON struct{ t *testing.T }

func (v forbiddenJSON) MarshalJSON() ([]byte, error) {
	v.t.Error("cancelled operation attempted serialization")
	return []byte(`1`), nil
}
func (v *forbiddenJSON) UnmarshalJSON([]byte) error {
	v.t.Error("cancelled operation attempted decoding")
	return nil
}

func TestContextCacheContract(t *testing.T) {
	for _, backend := range []string{"memory", "redis"} {
		t.Run(backend, func(t *testing.T) {
			var c ContextCache = NewMemoryCache()
			if backend == "redis" {
				mr := miniredis.RunT(t)
				r, err := NewRedisCache(RedisOptions{Addr: mr.Addr(), KeyPrefix: "ctx"})
				require.NoError(t, err)
				t.Cleanup(func() { _ = r.Close() })
				c = r
			}
			ctx := context.Background()
			var got string
			hit, err := c.GetContext(ctx, "missing", &got)
			require.NoError(t, err)
			require.False(t, hit)
			require.NoError(t, c.SetContext(ctx, "key", "value", time.Minute))
			hit, err = c.GetContext(ctx, "key", &got)
			require.NoError(t, err)
			require.True(t, hit)
			require.Equal(t, "value", got)
			var wrong int
			hit, err = c.GetContext(ctx, "key", &wrong)
			require.False(t, hit)
			var decode *json.UnmarshalTypeError
			require.ErrorAs(t, err, &decode)
			require.Error(t, c.SetContext(ctx, "bad", make(chan int), time.Minute))
			for _, deadline := range []bool{false, true} {
				cancelled, cancel := context.WithCancel(ctx)
				want := context.Canceled
				if deadline {
					cancel()
					cancelled, cancel = context.WithDeadline(ctx, time.Now().Add(-time.Second))
					want = context.DeadlineExceeded
				}
				cancel()
				require.ErrorIs(t, c.SetContext(cancelled, "key", forbiddenJSON{t}, time.Minute), want)
				hit, err = c.GetContext(cancelled, "key", &forbiddenJSON{t})
				require.False(t, hit)
				require.ErrorIs(t, err, want)
			}
			hit, err = c.GetContext(nil, "key", &got)
			require.NoError(t, err)
			require.True(t, hit)
			require.Equal(t, "value", got)
		})
	}
}

func TestRedisContextReadErrors(t *testing.T) {
	mr := miniredis.RunT(t)
	r, err := NewRedisCache(RedisOptions{Addr: mr.Addr()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })
	require.NoError(t, mr.Set("broken", "{"))
	var got any
	hit, err := r.GetContext(context.Background(), "broken", &got)
	require.False(t, hit)
	var syntax *json.SyntaxError
	require.ErrorAs(t, err, &syntax)
	require.False(t, r.Get("broken", &got))
	require.NoError(t, r.Close())
	hit, err = r.GetContext(context.Background(), "missing", &got)
	require.False(t, hit)
	require.Error(t, err, "closed pool is not a cache miss")
}

// A deadline can expire at the socket before the context timer gets scheduled.
type deadlineNotSignalled struct {
	context.Context
	at time.Time
}

func (c deadlineNotSignalled) Deadline() (time.Time, bool) { return c.at, true }
func TestRedisContextErrorDeadlineRace(t *testing.T) {
	socket := &net.OpError{Op: "read", Err: timeoutError{}}
	ctx := deadlineNotSignalled{context.Background(), time.Now().Add(-time.Second)}
	err := redisContextError(ctx, socket)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorIs(t, err, socket)
	ctx.at = time.Now().Add(time.Minute)
	require.False(t, errors.Is(redisContextError(ctx, socket), context.DeadlineExceeded))
	ordinary := errors.New("unrelated failure")
	require.Same(t, ordinary, redisContextError(ctx, ordinary))
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "socket timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

type contextProbe struct {
	calls int
	ctx   context.Context
}

func (p *contextProbe) DialHook(next redis.DialHook) redis.DialHook { return next }
func (p *contextProbe) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}
func (p *contextProbe) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		p.calls++
		p.ctx = ctx
		return redis.Nil // Observe the child without network or waiting five seconds.
	}
}

func TestRedisContextNoWorkAndOperationCeiling(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	t.Cleanup(func() { _ = rdb.Close() })
	probe := &contextProbe{}
	rdb.AddHook(probe)
	c := NewRedisCacheWithClient(rdb, "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var dest any
	_, err := c.GetContext(ctx, "key", &dest)
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, c.SetContext(ctx, "key", forbiddenJSON{t}, time.Minute), context.Canceled)
	require.Zero(t, probe.calls, "cancelled context must not enter Redis")
	for _, earlier := range []bool{false, true} {
		parent := context.Background()
		if earlier {
			var cancel context.CancelFunc
			parent, cancel = context.WithTimeout(parent, time.Second)
			defer cancel()
		}
		for _, get := range []bool{false, true} {
			start := time.Now()
			if get {
				_, _ = c.GetContext(parent, "key", &dest)
			} else {
				_ = c.SetContext(parent, "key", 1, time.Minute)
			}
			deadline, ok := probe.ctx.Deadline()
			require.True(t, ok)
			if earlier {
				want, _ := parent.Deadline()
				require.Equal(t, want, deadline)
			} else {
				require.False(t, deadline.Before(start.Add(redisOperationTimeout)))
				require.False(t, deadline.After(time.Now().Add(redisOperationTimeout)))
			}
			require.ErrorIs(t, probe.ctx.Err(), context.Canceled, "child resources released on return")
			require.NoError(t, parent.Err(), "child must not cancel caller")
		}
	}
}

func TestMemoryContextExpired(t *testing.T) {
	c := NewMemoryCache()
	require.NoError(t, c.SetContext(context.Background(), "key", 1, -time.Second))
	var dest int
	hit, err := c.GetContext(context.Background(), "key", &dest)
	require.NoError(t, err)
	require.False(t, hit)
	require.Empty(t, c.items)
}

func TestBorrowedRedisOptionsUnchanged(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", MaxRetries: 7, ContextTimeoutEnabled: false})
	t.Cleanup(func() { _ = rdb.Close() })
	before := *rdb.Options()
	_ = NewRedisCacheWithClient(rdb, "prefix")
	require.Equal(t, before.ContextTimeoutEnabled, rdb.Options().ContextTimeoutEnabled)
	require.Equal(t, before.MaxRetries, rdb.Options().MaxRetries)
	require.Equal(t, before.ReadTimeout, rdb.Options().ReadTimeout)
	require.Equal(t, before.WriteTimeout, rdb.Options().WriteTimeout)
	require.Equal(t, before.PoolTimeout, rdb.Options().PoolTimeout)
	require.Equal(t, before.DialTimeout, rdb.Options().DialTimeout)
}
