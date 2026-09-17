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

Requires Go 1.23 or higher.

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

### Rate limiting and request cancellation

`WithRateLimit(N, interval)` configures a per-client token bucket: an initial
burst of N requests, then sustained refill at **N / interval**, capped at N
available tokens. Fractional credit is retained until the bucket is full. The
default `WithRateLimit(50, time.Minute)` therefore earns one token every 1.2
seconds, sustaining 50 requests/minute after the initial burst (not a strict
rolling-window limit). Both arguments must be positive or `NewClient` returns an
error. The final rate option wins, including correcting an earlier invalid rate
option; it does not clear unrelated cache configuration errors.

`GetContext(ctx, endpoint)` lets cancellation or a deadline interrupt both the
rate-limit queue and the HTTP request. A caller cancelled while queued does not
reserve or consume a token. `Get(endpoint)` uses a background context and can
wait for rate capacity indefinitely. `WithTimeout` remains the **HTTP timeout**;
it does not bound time spent waiting for a rate-limit token. Use a caller context
when the entire `GetContext` operation needs a deadline.

Reuse a long-lived client: limits are **per SDK client**, not a shared
process/deployment-wide upstream budget. This corrects an older refill bug that
earned only one token per interval, so outgoing traffic can increase after
upgrading. Validate the configured aggregate upstream rate before rollout.

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
workflow on `main` verifies the suite, pushes the `v<VERSION>` tag, and
publishes GitHub release notes — no manual tagging. Merging without a bump
is a no-op. Consumers can also pin untagged commits directly
(`go get github.com/AbdoAnss/go-fantasy-pl@<ref>` resolves to a
pseudo-version).

## License

This project is licensed under the MIT License. See [LICENSE](LICENSE) for details.
