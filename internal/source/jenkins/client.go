// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package jenkins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const (
	// maxErrorBodySize limits how many bytes are read from error response bodies.
	maxErrorBodySize = 1024

	// treeQueryParam is the Jenkins query parameter that selects the fields of a response.
	treeQueryParam = "tree"

	// apiJSONPath is the suffix of every Jenkins JSON API endpoint.
	apiJSONPath = "/api/json"

	// acceptLanguageHeader and responseLanguage ask Jenkins for English responses, so that the
	// texts it localizes, such as the credential type names, do not depend on the locale of the
	// controller.
	acceptLanguageHeader = "Accept-Language"
	responseLanguage     = "en"
)

// statusError reports a non-2xx response returned by the Jenkins API.
type statusError struct {
	StatusCode int
	Body       string
}

// Error implements the error interface.
func (e *statusError) Error() string {
	return fmt.Sprintf("jenkins API returned status %d: %s", e.StatusCode, e.Body)
}

// client wraps an HTTP client with the Jenkins controller URL and credentials.
type client struct {
	baseURL    string
	username   string
	apiToken   string
	httpClient *http.Client
}

// getJSON executes an authenticated GET request for path, relative to the controller URL, and
// decodes the JSON body into target. A non-empty tree selects the fields of the response.
// The response headers are returned so callers can read controller metadata.
func (c *client) getJSON(ctx context.Context, path, tree string, target any) (http.Header, error) {
	var query url.Values
	if tree != "" {
		query = url.Values{}
		query.Set(treeQueryParam, tree)
	}

	resp, err := c.get(ctx, path, query, "application/json")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return nil, fmt.Errorf("failed to decode response of %s: %w", path, err)
	}

	return resp.Header, nil
}

// getText executes an authenticated GET request for path with query, and returns at most
// maxBytes of the plain text body. Reading is bounded on purpose: callers only keep a short
// prefix of the text.
func (c *client) getText(ctx context.Context, path string, query url.Values, maxBytes int64) (string, error) {
	resp, err := c.get(ctx, path, query, "text/plain")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return "", fmt.Errorf("failed to read response of %s: %w", path, err)
	}

	return string(body), nil
}

// get executes an authenticated GET request for path, relative to the controller URL, asking for
// an English response, and returns the response when its status is 2xx. The caller must close the response body.
func (c *client) get(ctx context.Context, path string, query url.Values, accept string) (*http.Response, error) {
	rawURL := c.baseURL + path
	if len(query) > 0 {
		rawURL += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request for %s: %w", path, err)
	}
	req.SetBasicAuth(c.username, c.apiToken)
	req.Header.Set("Accept", accept)
	req.Header.Set(acceptLanguageHeader, responseLanguage)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call %s: %w", path, err)
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodySize))
		return nil, fmt.Errorf("failed to call %s: %w", path, &statusError{StatusCode: resp.StatusCode, Body: string(body)})
	}

	return resp, nil
}

// isNotFound reports whether err carries a 404 response of the Jenkins API.
func isNotFound(err error) bool {
	var statusErr *statusError
	return errors.As(err, &statusErr) && statusErr.StatusCode == http.StatusNotFound
}

// jobPath returns the URL path of the item identified by the names of its ancestors and of the
// item itself. Every name is escaped, so names holding reserved characters, like the encoded
// branch names of multibranch projects, reach Jenkins unchanged.
func jobPath(segments []string) string {
	var builder strings.Builder
	for _, segment := range segments {
		builder.WriteString("/job/")
		builder.WriteString(url.PathEscape(segment))
	}
	return builder.String()
}

// appendSegment returns a new slice holding segments followed by name, never sharing the
// backing array of segments.
func appendSegment(segments []string, name string) []string {
	out := make([]string, 0, len(segments)+1)
	out = append(out, segments...)
	return append(out, name)
}
