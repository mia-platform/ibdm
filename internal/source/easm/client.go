// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package easm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

const (
	// maxErrorBodySize limits how many bytes we read from error response bodies
	// to avoid unbounded memory allocation on unexpectedly large payloads.
	maxErrorBodySize = 1024

	// nextCursorHeader carries the cursor for the next page; empty or absent on the last page.
	nextCursorHeader = "X-Next-Cursor"
	// cursorQueryParam names the query parameter used to request a specific page.
	cursorQueryParam = "cursor"
)

// client wraps an HTTP client with EASM endpoint configuration.
type client struct {
	baseURL  *url.URL
	dataPath string
	customer string
	token    string

	httpClient *http.Client
}

// newClient creates a client from the given config.
func newClient(cfg config) (*client, error) {
	u, err := url.Parse(cfg.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid EASM_BASE_URL: %w", ErrInvalidEnvVariable, err)
	}

	return &client{
		baseURL:  u,
		dataPath: cfg.DataPath,
		customer: cfg.Customer,
		token:    cfg.Token,
		httpClient: &http.Client{
			Timeout: cfg.HTTPTimeout,
		},
	}, nil
}

// dataPage is a single page of the /data response: a flat list of items plus
// the cursor for the next page (empty when this is the last page).
type dataPage struct {
	items      []map[string]any
	nextCursor string
}

// fetchDataPage retrieves a single page of items from the endpoint. Pass an
// empty cursor for the first page. "Latest completed run" is resolved
// server-side; the client never sees or picks a run id.
func (c *client) fetchDataPage(ctx context.Context, cursor string) (*dataPage, error) {
	u := c.baseURL.JoinPath(c.dataPath)
	if cursor != "" {
		q := u.Query()
		q.Set(cursorQueryParam, cursor)
		u.RawQuery = q.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")
	// X-Customer scopes the request to a single customer and is always set.
	// The bearer token authenticates the caller and is sent only once
	// configured — the backend has no auth yet.
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if c.customer != "" {
		req.Header.Set("X-Customer", c.customer)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, readErrorResponse(resp)
	}

	var items []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, fmt.Errorf("failed to decode data response: %w", err)
	}

	return &dataPage{
		items:      items,
		nextCursor: resp.Header.Get(nextCursorHeader),
	}, nil
}

// readErrorResponse constructs an error from a non-2xx HTTP response,
// reading at most maxErrorBodySize bytes from the body.
func readErrorResponse(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodySize))
	return fmt.Errorf("easm API returned status %d: %s", resp.StatusCode, string(body))
}
