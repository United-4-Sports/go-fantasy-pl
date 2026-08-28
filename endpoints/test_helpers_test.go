package endpoints_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AbdoAnss/go-fantasy-pl/client"
	"github.com/AbdoAnss/go-fantasy-pl/endpoints"
	"github.com/AbdoAnss/go-fantasy-pl/internal/cache"
	"github.com/stretchr/testify/require"
)

const liveTestEnv = "FPL_LIVE_TEST"

func skipUnlessLive(t *testing.T) {
	t.Helper()
	if os.Getenv(liveTestEnv) == "" {
		t.Skipf("set %s=1 to run live API tests", liveTestEnv)
	}
}

func newEndpointTestClient(t *testing.T) (*client.Client, *httptest.Server) {
	t.Helper()

	endpoints.SetSharedCache(cache.NewMemoryCache())

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.URL.Path == "/bootstrap-static/":
			writeTestdata(t, w, "bootstrap-static.json")
		case r.URL.Path == "/fixtures/":
			writeTestdata(t, w, "fixtures.json")
		case strings.HasPrefix(r.URL.Path, "/event/") && strings.HasSuffix(r.URL.Path, "/live/"):
			id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/event/"), "/live/")
			if _, err := os.Stat(filepath.Join("testdata", fmt.Sprintf("live-%s.json", id))); err != nil {
				http.NotFound(w, r)
				return
			}
			writeTestdata(t, w, fmt.Sprintf("live-%s.json", id))
		case strings.HasPrefix(r.URL.Path, "/element-summary/"):
			id := strings.TrimPrefix(strings.TrimSuffix(r.URL.Path, "/"), "/element-summary/")
			writeTestdata(t, w, fmt.Sprintf("element-summary-%s.json", id))
		default:
			http.NotFound(w, r)
		}
	}))

	c, err := client.NewClient(
		client.WithBaseURL(server.URL),
		client.WithMemoryCache(),
	)
	require.NoError(t, err)

	return c, server
}

func writeTestdata(t *testing.T, w http.ResponseWriter, name string) {
	t.Helper()

	_, err := w.Write(readTestdata(t, name))
	require.NoError(t, err)
}

// readTestdata returns the raw bytes of a committed capture. Fixtures are
// real API responses (refreshed via `make recapture`), so tests must assert
// schema and invariants rather than specific values.
func readTestdata(t *testing.T, name string) []byte {
	t.Helper()

	body, err := os.ReadFile(testdataPath(t, name))
	require.NoError(t, err)
	return body
}

// writeTestdataFile persists a fresh capture; used by the live harness in
// recapture mode.
func writeTestdataFile(t *testing.T, name string, body []byte) {
	t.Helper()

	require.NoError(t, os.WriteFile(testdataPath(t, name), body, 0o644))
}

// testdataPath builds the path for a capture file. Fixture names are
// sometimes built from API-supplied IDs (element-summary/<id>.json), so a
// plain name check keeps the path inside testdata/.
func testdataPath(t *testing.T, name string) string {
	t.Helper()

	if name == "" || strings.ContainsAny(name, `/\`) {
		t.Fatalf("invalid testdata fixture name %q", name)
	}
	return filepath.Join("testdata", name)
}

// jsonName formats a testdata filename.
func jsonName(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
}
