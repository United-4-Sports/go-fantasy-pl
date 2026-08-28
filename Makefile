.PHONY: test live-test recapture fmt fmt-check vet fix fix-check

# Hermetic suite: schema conformance + behavior tests against committed
# captures. No network, runs in CI.
test:
	go test -race ./...

# Live conformance: walks testdata/live_ids.json against the real FPL API
# and validates our models via the same conformance rules. Never blocks PRs.
live-test:
	FPL_LIVE_TEST=1 go test -race -count=1 -run TestLiveConformance ./endpoints/ -v

# Refresh the hermetic captures in testdata/ from the live API. The hermetic
# suite must stay green afterwards unless the schema actually changed.
recapture:
	FPL_LIVE_TEST=1 FPL_RECAPTURE=1 go test -count=1 -run TestLiveConformance ./endpoints/ -v

fmt:
	gofmt -w .

fmt-check:
	gofmt -l .

vet:
	go vet ./...

# Apply modernization fixes from the current Go toolchain (builtin min/max,
# interface{} -> any, range-over-int, reflect.TypeFor, ...).
fix:
	go fix ./...

# Verify the code is already up to date with `go fix`; fails if it changes
# anything. Used by CI.
fix-check:
	go fix ./...
	@git diff --exit-code -- '*.go' || (echo "'go fix' produced changes; run 'make fix' and commit them." && exit 1)
