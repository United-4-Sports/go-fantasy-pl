# go-fantasy-pl

[![Go Report Card](https://goreportcard.com/badge/github.com/AbdoAnss/go-fantasy-pl)](https://goreportcard.com/report/github.com/AbdoAnss/go-fantasy-pl)
[![Go Reference](https://pkg.go.dev/badge/github.com/AbdoAnss/go-fantasy-pl.svg)](https://pkg.go.dev/github.com/AbdoAnss/go-fantasy-pl)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

A feature-rich, high-performance Go SDK for the official [Fantasy Premier League API](https://fantasy.premierleague.com/api).

`go-fantasy-pl` provides a typed, idiomatic interface for interacting with FPL data. It includes built-in caching, automatic rate limiting, and asynchronous helpers for high-throughput workloads.

## Key Features

- Performance-first async helpers for fetching players, teams, and fixtures concurrently.
- Fully typed models for FPL API responses.
- Redis-first caching with automatic in-memory fallback for local development.
- Configurable timeouts, rate limits, and base URLs.
- Modular service-based architecture for testing and extension.

## Installation

```bash
go get github.com/AbdoAnss/go-fantasy-pl
```

Requires Go 1.24 or higher (the minimum is declared in `go.mod`; CI and the
release workflow additionally gate on the current stable toolchain).

## Quick Start

```go
package main

import (
	"fmt"
	"log"

	"github.com/AbdoAnss/go-fantasy-pl/client"
)

func main() {
	c, err := client.NewClient()
	if err != nil {
		log.Fatalf("failed to create client: %v", err)
	}

	teams, err := c.Teams.GetAllTeams()
	if err != nil {
		log.Fatalf("failed to fetch teams: %v", err)
	}

	players, err := c.Players.GetAllPlayers()
	if err != nil {
		log.Fatalf("failed to fetch players: %v", err)
	}

	fmt.Printf("Successfully loaded %d teams and %d players.\n", len(teams), len(players))
}
```

## Live Gameweek Points

`c.Live` exposes the gameweek live endpoint (`/event/{id}/live/`), which
reports in-play points and stats for every player. A manager's live total is
computed by joining these stats with their picks (multiplier 2 for captain,
0 for bench):

```go
live, err := c.Live.GetEventLive(currentGameweek)
picks, err := c.Managers.GetCurrentTeam(managerID)

total := 0
for _, pick := range picks.Picks {
	if pts, ok := live.PointsFor(pick.Element); ok {
		total += pts * pick.Multiplier
	}
}
```

Freshness caveats: bonus points are provisional while fixtures are in play,
the upstream CDN may serve data up to a few minutes old, and the SDK caches
responses for only 30 seconds to stay close to the source.

## Caching

`client.NewClient()` now prefers Redis by default.

- If Redis is reachable, the SDK uses it automatically.
- If Redis is not reachable, the SDK falls back to the in-memory cache.
- If you want Redis to be mandatory, set `FPL_CACHE_BACKEND=redis`.
- If you want to force in-memory caching, use `client.WithMemoryCache()` or set `FPL_CACHE_BACKEND=memory`.

Cache selection is resolved at construction and kept for the client's
lifetime; later constructions never redirect an existing client. Clients
using the same store (in-memory) or Redis DB + key prefix share entries, so
one client's warming serves another. Current-team picks are cached per
manager and gameweek (`manager_team_{id}_gw{n}`); old `manager_team_{id}`
entries expire naturally and are never cleared.

`Close()` releases only what the SDK created: an SDK-dialed Redis pool and idle
connections on the SDK-created HTTP transport. Caller-supplied pools, caches,
and `http.Client`s are never closed, and closing one client never clears the
shared in-memory store. Stop in-flight requests (cancel their contexts) before
calling it; `Close()` is idempotent.

### Environment Variables

```bash
export REDIS_ADDR=localhost:6379
export REDIS_PASSWORD=
export REDIS_DB=0
export REDIS_KEY_PREFIX=go-fantasy-pl
export FPL_CACHE_BACKEND=auto
```

Supported `FPL_CACHE_BACKEND` values:

- `auto`: try Redis first, then fall back to memory.
- `redis`: require Redis and fail client creation if it is unavailable.
- `memory`: skip Redis and use the in-memory cache.

### Explicit Redis Configuration

```go
c, err := client.NewClient(
	client.WithRedisCache(client.RedisOptions{
		Addr:      "localhost:6379",
		Password:  "",
		DB:        0,
		KeyPrefix: "go-fantasy-pl",
	}),
)
```

## Advanced Usage

### Asynchronous Data Retrieval

```go
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/AbdoAnss/go-fantasy-pl/client"
)

func main() {
	c, err := client.NewClient()
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	playersCh := c.Players.GetAllPlayersAsync(ctx)
	teamsCh := c.Teams.GetAllTeamsAsync(ctx)
	fixturesCh := c.Fixtures.GetAllFixturesAsync(ctx)

	playersRes := <-playersCh
	teamsRes := <-teamsCh
	fixturesRes := <-fixturesCh

	if playersRes.Err != nil || teamsRes.Err != nil || fixturesRes.Err != nil {
		log.Fatal("one or more requests failed")
	}

	fmt.Printf(
		"Fetched %d players, %d teams, and %d fixtures concurrently!\n",
		len(playersRes.Value),
		len(teamsRes.Value),
		len(fixturesRes.Value),
	)
}
```

### Configuration Options

```go
c, err := client.NewClient(
	client.WithTimeout(30*time.Second),
	client.WithRateLimit(100, time.Minute),
	client.WithBaseURL("https://custom-proxy.example/api"),
)
```

### Rate limiting

`WithRateLimit(N, interval)` permits an initial burst of N requests, then sustains N requests per
interval. Both arguments must be positive or `NewClient` returns an error. `GetContext` cancels queued
waits via its context without consuming a token; `WithTimeout` bounds only the HTTP request. Limits are
per client — reuse one long-lived client instead of creating one per request.

### Throttle handling (403/429)

The FPL API has no published rate limits; heavy clients get heuristic temporary 403 blocks and
occasional 429s. The client handles both automatically and is on by default:

- **429**: the `Retry-After` header (delay-seconds or HTTP-date) is waited out exactly — capped at
  2 minutes — and the request is retried once. Without the header, exponential backoff applies.
  Numeric values too large for `time.Duration` are saturated to MaxInt64 nanoseconds before the cap;
  observer events report the saturated `RetryAfter` and the capped `Wait`.
- **403**: treated as throttling; exponential backoff with ±50% jitter — 500ms, then 1s — for up
  to two retries, then the response is surfaced to the caller.

All waits are cancellable through the request context, every retry passes through the rate limiter
(retries consume tokens), and non-throttle statuses (404, 5xx, …) reach callers on the first
attempt with unchanged error shapes. Tune or disable with `WithThrottlePolicy`; the zero value
keeps the defaults and `-1` disables one status class:

```go
c, err := client.NewClient(
	client.WithThrottlePolicy(client.ThrottlePolicy{
		Max403Retries: 4,             // more patience with 403 blocks
		Max429Retries: -1,            // never retry 429s
	}),
	client.WithThrottleObserver(func(e client.ThrottleEvent) {
		// nonblocking metering: e.Endpoint, e.StatusCode, e.Attempt,
		// e.Wait, e.RetryAfter, e.WillRetry — never response bodies
	}),
)
```

`ThrottlePolicy{Disabled: true}` restores pre-throttle single-attempt behavior, which hermetic
tests and replay upstreams may prefer.

## CI/CD

The GitHub Actions pipeline now covers:

- formatting, module tidy checks, vet, tests, and builds on pull requests and pushes
- SonarCloud analysis (quality gate + coverage from `coverage.out`; see `sonar-project.properties`)
- optional deployment webhook triggering on `main`

Optional repository secrets:

- `SONAR_TOKEN`
- `DEPLOY_WEBHOOK_URL`
- `DEPLOY_WEBHOOK_TOKEN`

## Project Structure

- `client/`: main entry point and configuration
- `endpoints/`: domain-specific service implementations
- `models/`: data structures mapping to FPL API responses
- `internal/cache/`: cache implementations
- `examples/`: example programs

## Contributing

Contributions are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## Releasing

Releases are driven by the `VERSION` file: bump it in a PR, and the release
workflow on `main` runs the publication gate on the minimum and current
stable Go toolchains, pushes the `v<VERSION>` tag, and publishes GitHub
release notes — no manual tagging. Merging without a bump is a no-op, and a
tag whose release half-failed is recovered by publishing the release for
the tagged commit; existing tags are never moved. Consumers can also pin
untagged commits directly (`go get github.com/AbdoAnss/go-fantasy-pl@<ref>`
resolves to a pseudo-version).

## License

This project is licensed under the MIT License. See [LICENSE](LICENSE) for details.
