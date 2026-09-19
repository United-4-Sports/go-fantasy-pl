package cache

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// blockedRedisTransport is a real loopback TCP RESP server, not a Redis hook or
// simulated clock. It completes connection initialization and PING, then reads
// GET/SET without replying. Thus only client socket/context deadlines unblock I/O.
func blockedRedisTransport(t *testing.T) (string, <-chan string, *atomic.Int32) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	entered := make(chan string, 16)
	stop := make(chan struct{})
	var calls atomic.Int32
	var wg sync.WaitGroup
	var mu sync.Mutex
	var conns []net.Conn
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, conn)
			mu.Unlock()
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer conn.Close()
				reader := bufio.NewReader(conn)
				for {
					args, err := readRESPCommand(reader)
					if err != nil {
						return
					}
					command := strings.ToLower(args[0])
					switch command {
					case "get", "set":
						calls.Add(1)
						select {
						case entered <- command:
						case <-stop:
							return
						}
						<-stop
						return
					case "hello":
						_, err = io.WriteString(conn, "-ERR unknown command 'hello'\r\n")
					case "ping":
						_, err = io.WriteString(conn, "+PONG\r\n")
					default:
						_, err = io.WriteString(conn, "+OK\r\n")
					}
					if err != nil {
						return
					}
				}
			}()
		}
	}()
	t.Cleanup(func() {
		close(stop)
		_ = listener.Close()
		mu.Lock()
		for _, conn := range conns {
			_ = conn.Close()
		}
		mu.Unlock()
		wg.Wait()
	})
	return listener.Addr().String(), entered, &calls
}

func readRESPCommand(reader *bufio.Reader) ([]string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	if len(line) < 3 || line[0] != '*' {
		return nil, fmt.Errorf("invalid array")
	}
	n, err := strconv.Atoi(strings.TrimSpace(line[1:]))
	if err != nil || n < 1 || n > 100 {
		return nil, fmt.Errorf("invalid array length")
	}
	args := make([]string, n)
	for i := range args {
		line, err = reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		if len(line) < 3 || line[0] != '$' {
			return nil, fmt.Errorf("invalid bulk string")
		}
		size, err := strconv.Atoi(strings.TrimSpace(line[1:]))
		if err != nil || size < 0 || size > 1<<20 {
			return nil, fmt.Errorf("invalid bulk length")
		}
		data := make([]byte, size+2)
		if _, err = io.ReadFull(reader, data); err != nil {
			return nil, err
		}
		args[i] = string(data[:size])
	}
	return args, nil
}

func TestRedisContextBlockedTransportDeadline(t *testing.T) {
	for _, owned := range []bool{true, false} {
		for _, operation := range []string{"get", "set"} {
			t.Run(fmt.Sprintf("owned=%t/%s", owned, operation), func(t *testing.T) {
				addr, entered, calls := blockedRedisTransport(t)
				var c *RedisCache
				if owned {
					var err error
					c, err = NewRedisCache(RedisOptions{Addr: addr})
					require.NoError(t, err)
				} else {
					rdb := redis.NewClient(&redis.Options{
						Addr: addr, ContextTimeoutEnabled: true, MaxRetries: -1,
						DialTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second,
						WriteTimeout: 5 * time.Second, PoolTimeout: 5 * time.Second,
					})
					c = NewRedisCacheWithClient(rdb, "borrowed")
					require.NoError(t, rdb.Ping(context.Background()).Err())
				}
				t.Cleanup(func() { _ = c.Close() })
				require.True(t, c.client.Options().ContextTimeoutEnabled)
				// go-redis normalizes configured -1 to zero retries.
				require.Zero(t, c.client.Options().MaxRetries)
				ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
				defer cancel()
				result := make(chan error, 1)
				go func() {
					if operation == "set" {
						result <- c.SetContext(ctx, "key", "value", time.Minute)
					} else {
						var dest string
						_, err := c.GetContext(ctx, "key", &dest)
						result <- err
					}
				}()
				select {
				case command := <-entered:
					require.Equal(t, operation, command)
				case <-time.After(2 * time.Second):
					t.Fatal("command never reached TCP server")
				}
				select {
				case err := <-result:
					require.ErrorIs(t, err, context.DeadlineExceeded)
					deadline, _ := ctx.Deadline()
					require.False(t, time.Now().Before(deadline), "must expire caller deadline, not fail fast")
				case <-time.After(2 * time.Second):
					t.Fatal("caller deadline did not interrupt blocked Redis I/O")
				}
				require.EqualValues(t, 1, calls.Load(), "cache command must not retry")
			})
		}
	}
}
