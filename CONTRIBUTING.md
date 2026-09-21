# Contributing to go-fantasy-pl

Thanks for helping improve this project.

## Quick Start

```bash
git clone https://github.com/AbdoAnss/go-fantasy-pl.git
cd go-fantasy-pl
go mod download
go test ./...
```

Use Go `1.24+`. The minimum supported version is declared in `go.mod`;
CI and the release workflow test both that minimum and the current stable
toolchain, so keep `go.mod`'s `go` directive as the single source of truth
for the floor.

## Workflow

1. Create a branch from `main`.
2. Make focused changes.
3. Add or update tests.
4. Run checks locally.
5. Open a PR with a clear description.

Branch name examples:

- `feat/async-docs`
- `fix/cache-error-handling`
- `chore/test-cleanup`

## Local Checks

Run these before pushing:

```bash
gofmt -w .
go test ./...
```

Or with the Makefile: `make test`, `make live-test`, `make recapture`.

If you have `golangci-lint` installed:

```bash
golangci-lint run
```

## Testing Strategy

Our contract with the FPL API is schema-shaped, not value-shaped: we care
when the API adds, removes, or renames fields — never about specific player
or team values. Tests therefore assert schema and invariants, never
real-world literals like team names or match IDs.

Three tiers:

1. **Hermetic schema conformance** (runs in CI). Every committed capture in
   `endpoints/testdata/` is decoded into our models and validated by
   `internal/conformance.Check`: every API key must map to a model field (or
   an explicit allowlist entry in `endpoints/conformance_specs_test.go`),
   every model field must have an API key, and scalar values must match.
2. **Hermetic behavior tests.** Endpoints are exercised via `httptest`
   servers serving the committed captures. Expectations (counts, IDs) are
   derived from the capture files themselves or from invariants, so
   refreshing captures never breaks them.
3. **Live conformance** (opt-in, never blocks PRs). `make live-test` walks
   `endpoints/testdata/live_ids.json` against the real API and applies the
   same conformance rules. Leagues are purged between seasons; a 404 for a
   configured ID is a skip, not a failure — update `live_ids.json` at the
   start of each season. A scheduled workflow runs this weekly and opens an
   issue on drift.

### Refreshing fixtures

```bash
make recapture
```

Re-fetches the live payloads and rewrites the captures in
`endpoints/testdata/`. The hermetic suite must stay green afterwards; if it
fails, the schema genuinely changed — update the models or the allowlists in
`endpoints/conformance_specs_test.go` (each allowlist entry is a deliberate,
reviewable decision not to map that field).

## Code Guidelines

- Keep APIs simple and predictable.
- Prefer small, testable functions.
- Handle errors explicitly.
- Add comments only where logic is non-obvious.
- Keep examples in `examples/` working.

## Pull Requests

Please include:

- What changed
- Why it changed
- Any breaking change notes
- Linked issue (if applicable)

PR checklist:

- Tests pass
- Docs updated when behavior changes
- No unrelated refactors

## Issues

Open an issue for bugs, feature requests, or documentation improvements.

## License

By contributing, you agree your contributions are licensed under the [MIT License](LICENSE).
