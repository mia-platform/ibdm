// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package bitbucket

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// ErrIteratorDone signals that all pages have been consumed.
var ErrIteratorDone = errors.New("iterator done")

// iterator provides page-by-page access to a paginated Bitbucket API resource.
type iterator interface {
	// next returns the next page of items. Returns ErrIteratorDone when all
	// pages have been consumed. The caller never receives an empty slice.
	next(ctx context.Context) ([]map[string]any, error)
}

// pageIterator implements iterator using Bitbucket's next-URL pagination.
// The nextURL is the full URL for the next request; when it becomes empty
// (no "next" field in the response), the iterator is done.
type pageIterator struct {
	client  *client
	nextURL string
	done    bool
}

// maxErrorBodySize limits how many bytes we read from error response bodies
// to avoid unbounded memory allocation on unexpectedly large payloads.
const maxErrorBodySize = 1024

// next fetches the next page of results from the Bitbucket API. It returns
// ErrIteratorDone when all pages have been consumed or when an empty page
// is received. On error, the iterator is marked as done to prevent retries.
func (it *pageIterator) next(ctx context.Context) ([]map[string]any, error) {
	if it.done {
		return nil, ErrIteratorDone
	}

	resp, err := it.client.doRequest(ctx, it.nextURL)
	if err != nil {
		it.done = true
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		it.done = true
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodySize))
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var page struct {
		Values []map[string]any `json:"values"`
		Next   string           `json:"next"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		it.done = true
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(page.Values) == 0 {
		it.done = true
		return nil, ErrIteratorDone
	}

	if page.Next == "" {
		it.done = true
	} else {
		it.nextURL = page.Next
	}

	return page.Values, nil
}
