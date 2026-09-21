package endpoints

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/AbdoAnss/go-fantasy-pl/api"
)

// fetchSpec names one fetch-and-decode operation for its error messages and
// maps non-200 statuses to domain errors. A nil hook falls through to the
// generic "unexpected status code" error, matching the per-method switches
// this factored out.
type fetchSpec struct {
	// fetch prefixes the transport-failure error, e.g. "failed to get
	// manager data".
	fetch string
	// decode prefixes the JSON-decode error, e.g. "failed to decode league
	// data".
	decode string
	// notFound maps HTTP 404 to a domain error (e.g. ErrLeagueNotFound).
	notFound func() error
	// badRequest maps HTTP 400; used by H2H matches, whose 400s carry a
	// query-rejection detail payload.
	badRequest func(resp *http.Response) error
}

// fetchJSON performs the GET + status policy + decode flow shared by the
// manager and league lookups: it fetches endpoint with ctx, applies the
// spec's status mapping, and unmarshals the body into dest. The body is
// fully read and closed before returning, so callers never hold it open.
func fetchJSON[T any](ctx context.Context, c api.Client, endpoint string, spec fetchSpec, dest *T) error {
	resp, err := c.GetContext(ctx, endpoint)
	if err != nil {
		return fmt.Errorf("%s: %w", spec.fetch, err)
	}
	defer resp.Body.Close()

	if err := statusError(resp, spec); err != nil {
		return err
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}
	if err := json.Unmarshal(body, dest); err != nil {
		return fmt.Errorf("%s: %w", spec.decode, err)
	}
	return nil
}

// statusError maps a non-200 response to the spec's domain errors, or to the
// generic unexpected-status error when no hook applies.
func statusError(resp *http.Response, spec fetchSpec) error {
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusNotFound:
		if spec.notFound != nil {
			return spec.notFound()
		}
	case http.StatusBadRequest:
		if spec.badRequest != nil {
			return spec.badRequest(resp)
		}
	}
	return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
}
