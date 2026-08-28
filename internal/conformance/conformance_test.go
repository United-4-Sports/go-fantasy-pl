package conformance

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

type pageFixture struct {
	HasNext bool   `json:"has_next"`
	Page    int    `json:"page"`
	Name    string `json:"name"`
	Winner  *int   `json:"winner"`
}

type timeFixture struct {
	Deadline    time.Time  `json:"deadline"`
	KickoffTime *time.Time `json:"kickoff_time"`
}

func TestCheck_TimestampFidelity(t *testing.T) {
	deadline := "2026-08-15T17:30:00Z"
	raw := `{"deadline": "` + deadline + `", "kickoff_time": "` + deadline + `"}`
	decoded := decodeInto[timeFixture](t, raw)

	failures := runCheck(t, raw, Spec{Model: &decoded})
	if len(failures) != 0 {
		t.Fatalf("expected conforming timestamps to pass, got: %v", failures)
	}

	stale := `{"deadline": "2026-08-16T17:30:00Z", "kickoff_time": null}`
	failures = runCheck(t, stale, Spec{Model: &decoded})
	if !anyContains(failures, "timeFixture.Deadline does not match payload value") {
		t.Fatalf("expected timestamp fidelity failure, got: %v", failures)
	}
}

// decodeInto mirrors real usage: the model under test is produced by
// encoding/json from the payload, never hand-constructed.
func decodeInto[T any](t *testing.T, raw string) T {
	t.Helper()

	var v T
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatalf("failed to decode raw payload: %v", err)
	}
	return v
}

func runCheck(t *testing.T, raw string, spec Spec) []string {
	t.Helper()

	mock := &mockTB{TB: t}
	func() {
		defer mock.recoverFatal()
		Check(mock, []byte(raw), spec)
	}()
	return mock.failures
}

// mockTB captures Errorf calls so tests can assert on individual violations.
type mockTB struct {
	testing.TB
	failures []string
}

func (m *mockTB) Errorf(format string, args ...any) {
	m.failures = append(m.failures, fmt.Sprintf(format, args...))
}

func (m *mockTB) Fatalf(format string, args ...any) {
	m.failures = append(m.failures, fmt.Sprintf(format, args...))
	panic(nil) // stop Check the way a real Fatalf would
}

func (m *mockTB) recoverFatal() {
	_ = recover()
}

func anyContains(list []string, substr string) bool {
	for _, s := range list {
		if strings.Contains(s, substr) {
			return true
		}
	}
	return false
}

func TestCheck_PassesOnConformingPayload(t *testing.T) {
	raw := `{"has_next": true, "page": 3, "name": "anything", "winner": 7}`
	decoded := decodeInto[pageFixture](t, raw)

	failures := runCheck(t, raw, Spec{Model: &decoded})
	if len(failures) != 0 {
		t.Fatalf("expected no failures, got: %v", failures)
	}
}

func TestCheck_UnmappedAPIKeyFails(t *testing.T) {
	raw := `{"has_next": true, "page": 1, "name": "x", "winner": null, "seed_value": 4}`
	decoded := decodeInto[pageFixture](t, raw)

	failures := runCheck(t, raw, Spec{Model: &decoded})
	if !anyContains(failures, `API key "seed_value" is not mapped`) {
		t.Fatalf("expected unmapped-key failure, got: %v", failures)
	}
}

func TestCheck_AllowlistSuppressesUnmappedKey(t *testing.T) {
	raw := `{"has_next": true, "page": 1, "name": "x", "winner": null, "seed_value": 4}`
	decoded := decodeInto[pageFixture](t, raw)

	failures := runCheck(t, raw, Spec{Model: &decoded, Allowlist: []string{"seed_value"}})
	if len(failures) != 0 {
		t.Fatalf("expected allowlisted key to pass, got: %v", failures)
	}
}

func TestCheck_MistypedTagFailsBothWays(t *testing.T) {
	// A tag typo leaves the model field orphaned and the API key unmapped;
	// both rules must fire.
	raw := `{"has_next": true, "page": 1, "name": "x", "winer": null}`
	decoded := decodeInto[pageFixture](t, raw)

	failures := runCheck(t, raw, Spec{Model: &decoded})
	if !anyContains(failures, `API key "winer" is not mapped`) {
		t.Fatalf("expected unmapped-key failure, got: %v", failures)
	}
	if !anyContains(failures, `field conformance.pageFixture.Winner has no corresponding API key`) {
		t.Fatalf("expected orphaned-field failure, got: %v", failures)
	}
}

func TestCheck_OrphanedModelFieldFails(t *testing.T) {
	// The payload lacks "name": the API removed or renamed the key, so the
	// model field silently decodes to zero.
	raw := `{"has_next": true, "page": 1, "winner": null}`
	decoded := decodeInto[pageFixture](t, raw)

	failures := runCheck(t, raw, Spec{Model: &decoded})
	if !anyContains(failures, `field conformance.pageFixture.Name has no corresponding API key`) {
		t.Fatalf("expected orphaned-field failure, got: %v", failures)
	}
}

func TestCheck_FidelityCatchesStaleModel(t *testing.T) {
	// The model was decoded from a different payload than the one checked —
	// e.g. a stale fixture or live data that changed between two fetches.
	modelRaw := `{"has_next": true, "page": 2, "name": "league-77", "winner": null}`
	checkRaw := `{"has_next": true, "page": 2, "name": "other-league", "winner": null}`
	decoded := decodeInto[pageFixture](t, modelRaw)

	failures := runCheck(t, checkRaw, Spec{Model: &decoded})
	if !anyContains(failures, `field conformance.pageFixture.Name does not match payload value other-league`) {
		t.Fatalf("expected fidelity failure on Name, got: %v", failures)
	}
}

func TestCheck_NullPayloadValueTolerated(t *testing.T) {
	raw := `{"has_next": false, "page": 1, "name": "x", "winner": null}`
	decoded := decodeInto[pageFixture](t, raw)

	failures := runCheck(t, raw, Spec{Model: &decoded})
	if len(failures) != 0 {
		t.Fatalf("null values must be tolerated, got: %v", failures)
	}
}

func TestCheck_NilPointerWithNonNullPayloadFails(t *testing.T) {
	modelRaw := `{"has_next": false, "page": 1, "name": "x", "winner": null}`
	checkRaw := `{"has_next": false, "page": 1, "name": "x", "winner": 3}`
	decoded := decodeInto[pageFixture](t, modelRaw)

	failures := runCheck(t, checkRaw, Spec{Model: &decoded})
	if !anyContains(failures, `conformance.pageFixture.Winner does not match payload value 3`) {
		t.Fatalf("expected nil-pointer fidelity failure, got: %v", failures)
	}
}

func TestCheck_SliceModelChecksFirstElement(t *testing.T) {
	raw := `[{"has_next": true, "page": 1, "name": "a", "winner": null}, {"has_next": false, "page": 2, "name": "b", "winner": null}]`
	decoded := decodeInto[[]pageFixture](t, raw)

	failures := runCheck(t, raw, Spec{Model: decoded})
	if len(failures) != 0 {
		t.Fatalf("expected conforming slice to pass, got: %v", failures)
	}
}

func TestCheck_SliceLengthMismatchFails(t *testing.T) {
	raw := `[{"has_next": true, "page": 1, "name": "a", "winner": null}, {"has_next": false, "page": 2, "name": "b", "winner": null}]`
	decoded := []pageFixture{{HasNext: true, Page: 1, Name: "a"}}

	failures := runCheck(t, raw, Spec{Model: decoded})
	if !anyContains(failures, "model has 1 elements, payload has 2") {
		t.Fatalf("expected length mismatch failure, got: %v", failures)
	}
}

func TestCheck_EmptyModelSliceWithNonEmptyPayloadDoesNotPanic(t *testing.T) {
	// The classic stale-model scenario: the payload has elements but the
	// decoded slice is empty. Check must report the mismatch, not panic.
	raw := `[{"has_next": true, "page": 1, "name": "a", "winner": null}]`
	decoded := []pageFixture{}

	failures := runCheck(t, raw, Spec{Model: decoded})
	if !anyContains(failures, "model has 0 elements, payload has 1") {
		t.Fatalf("expected length mismatch failure, got: %v", failures)
	}
}

func TestCheck_PointerSliceModel(t *testing.T) {
	raw := `[{"has_next": true, "page": 1, "name": "a", "winner": null}]`
	decoded := decodeInto[[]*pageFixture](t, raw)

	failures := runCheck(t, raw, Spec{Model: decoded})
	if len(failures) != 0 {
		t.Fatalf("expected []*T slice model to pass, got: %v", failures)
	}
}

func TestReport_NilModelFailsCleanly(t *testing.T) {
	mock := &mockTB{TB: t}
	func() {
		defer mock.recoverFatal()
		Report(mock, []byte(`{"a":1}`), Spec{Model: (*pageFixture)(nil)})
	}()
	if !anyContains(mock.failures, "Spec.Model must be a populated struct") {
		t.Fatalf("expected clean fatal for nil model, got: %v", mock.failures)
	}
}

func TestReport_PointerSliceModel(t *testing.T) {
	raw := `{"has_next": true, "page": 1, "name": "x", "winner": null, "extra": 1}`
	decoded := decodeInto[[]*pageFixture](t, `[{"has_next": true, "page": 1, "name": "x", "winner": null}]`)

	unmapped := Report(t, []byte(raw), Spec{Model: decoded})
	if strings.Join(unmapped, ",") != "extra" {
		t.Fatalf("expected [extra] for []*T model, got %v", unmapped)
	}
}

func TestExtract(t *testing.T) {
	raw := []byte(`{"results": [{"id": 1}, {"id": 2}], "page": 3}`)

	got := Extract(t, raw, "results", 1)
	if string(got) != `{"id":2}` {
		t.Fatalf("unexpected extract result: %s", got)
	}

	got = Extract(t, raw, "page")
	if string(got) != `3` {
		t.Fatalf("unexpected scalar extract result: %s", got)
	}
}

func TestReport_ListsUnmappedKeys(t *testing.T) {
	raw := `{"has_next": true, "page": 1, "name": "x", "winner": null, "zz": 1, "aa": 2}`
	decoded := decodeInto[pageFixture](t, raw)

	unmapped := Report(t, []byte(raw), Spec{Model: &decoded})
	if strings.Join(unmapped, ",") != "aa,zz" {
		t.Fatalf("expected sorted unmapped keys [aa zz], got %v", unmapped)
	}
}

type uintFixture struct {
	Count uint `json:"count"`
}

func TestCheck_UnsignedFields(t *testing.T) {
	raw := `{"count": 7}`
	decoded := decodeInto[uintFixture](t, raw)

	failures := runCheck(t, raw, Spec{Model: &decoded})
	if len(failures) != 0 {
		t.Fatalf("expected unsigned field to pass, got: %v", failures)
	}

	stale := decodeInto[uintFixture](t, `{"count": 9}`)
	failures = runCheck(t, raw, Spec{Model: &stale})
	if !anyContains(failures, "uintFixture.Count does not match payload value 7") {
		t.Fatalf("expected unsigned fidelity failure, got: %v", failures)
	}
}

func TestExtract_ErrorPaths(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		path []any
		want string
	}{
		{"index into object", `{"a": 1}`, []any{0}, "payload node is not an array"},
		{"key into array", `[1, 2]`, []any{"a"}, "payload node is not an object"},
		{"index out of range", `{"r": [1]}`, []any{"r", 5}, "index out of range"},
		{"null node", `{"a": null}`, []any{"a"}, "leads to a null node"},
		{"bad segment type", `{"a": 1}`, []any{1.5}, "path segments must be string or int"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mock := &mockTB{TB: t}
			func() {
				defer mock.recoverFatal()
				Extract(mock, []byte(tc.raw), tc.path...)
			}()
			if !anyContains(mock.failures, tc.want) {
				t.Fatalf("expected %q, got: %v", tc.want, mock.failures)
			}
		})
	}
}

// scalarFixture exercises the scalar comparison helpers' mismatch branches:
// every case pairs a model decoded from a valid payload with a raw payload
// whose corresponding value has the wrong type or value.
type scalarFixture struct {
	Name string    `json:"name"`
	On   bool      `json:"on"`
	When time.Time `json:"when"`
}

func TestCheck_ScalarMismatches(t *testing.T) {
	valid := `{"name": "x", "on": true, "when": "2026-08-15T17:30:00Z"}`
	decoded := decodeInto[scalarFixture](t, valid)

	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"bool payload for string field", `{"name": true, "on": true, "when": "2026-08-15T17:30:00Z"}`, "payload is a bool but the field is string"},
		{"string value drift", `{"name": "y", "on": true, "when": "2026-08-15T17:30:00Z"}`, `field has "x"`},
		{"string payload for bool field", `{"name": "x", "on": "yes", "when": "2026-08-15T17:30:00Z"}`, "payload is a string but the field is bool"},
		{"bool value drift", `{"name": "x", "on": false, "when": "2026-08-15T17:30:00Z"}`, "field has true"},
		{"number payload for time field", `{"name": "x", "on": true, "when": 42}`, "field is a time.Time"},
		{"malformed timestamp", `{"name": "x", "on": true, "when": "not-a-time"}`, "is not RFC 3339"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			failures := runCheck(t, tc.raw, Spec{Model: &decoded})
			if !anyContains(failures, tc.want) {
				t.Fatalf("expected %q, got: %v", tc.want, failures)
			}
		})
	}
}
