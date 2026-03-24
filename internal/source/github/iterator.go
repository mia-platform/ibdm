// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
)

// relNextRegex matches the URL in a Link header with rel="next".
var relNextRegex = regexp.MustCompile(`(?i)<([^>]*)>\s*;\s*rel="next"`)

// iterator provides page-by-page access to a paginated GitHub API resource.
type iterator interface {
	// next returns the next page of items. Returns ErrIteratorDone when all
	// pages have been consumed. The caller never receives an empty slice —
	// the iterator converts empty pages to ErrIteratorDone internally.
	next(ctx context.Context) ([]map[string]any, error)
}

// pageIterator implements the iterator interface using page-number-based
// pagination with the Link header stop condition.
type pageIterator struct {
	client     *client
	path       string
	apiVersion string
	currPage   int
	done       bool
}

// maxErrorBodySize limits how many bytes we read from error response bodies
// to avoid unbounded memory allocation on unexpectedly large payloads.
const maxErrorBodySize = 1024

func (it *pageIterator) next(ctx context.Context) ([]map[string]any, error) {
	if it.done {
		return nil, ErrIteratorDone
	}

	it.currPage++

	resp, err := it.client.doRequest(ctx, it.path, it.apiVersion, it.currPage)
	if err != nil {
		it.done = true
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		remaining := resp.Header.Get("X-Ratelimit-Remaining")
		if remaining == "0" {
			it.done = true
			reset := resp.Header.Get("X-Ratelimit-Reset")
			body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodySize))
			return nil, fmt.Errorf("rate limit exhausted (resets at %s): %s", reset, string(body))
		}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		it.done = true
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodySize))
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var items []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		it.done = true
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(items) == 0 {
		it.done = true
		return nil, ErrIteratorDone
	}

	linkHeader := resp.Header.Get("Link")
	if !hasRelNext(linkHeader) {
		it.done = true
	}

	return items, nil
}

// hasRelNext reports whether the Link header contains a rel="next" entry.
func hasRelNext(linkHeader string) bool {
	if linkHeader == "" {
		return false
	}
	return relNextRegex.MatchString(linkHeader)
}

// ErrIteratorDone signals that all pages have been consumed.
var ErrIteratorDone = errors.New("iterator done")
