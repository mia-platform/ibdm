// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package gitlab

import (
	"context"
	"errors"
)

var (
	// ErrIteratorDone is returned by iterator next() methods when all pages have been consumed.
	ErrIteratorDone = errors.New("iterator done")
)

// iterator defines the contract for paginated GitLab API iterators.
type iterator interface {
	next(ctx context.Context) ([]map[string]any, error)
}

var _ iterator = &pageIterator{}

// pageIterator pages through a GitLab list endpoint. The path is fixed at
// construction time by the client layer.
type pageIterator struct {
	c          *gitLabClient
	path       string
	currPage   int
	totalPages int
	done       bool
}

// next fetches the next page of items. Returns ErrIteratorDone when all pages
// have been consumed. The caller never receives an empty slice.
func (it *pageIterator) next(ctx context.Context) ([]map[string]any, error) {
	if it.done {
		return nil, ErrIteratorDone
	}

	it.currPage++

	items, totalPages, err := it.c.makePageableRequest(ctx, it.path, it.currPage)
	if err != nil {
		if it.currPage >= it.totalPages {
			it.done = true
		}
		return nil, err
	}

	it.totalPages = totalPages

	if it.currPage >= it.totalPages {
		it.done = true
	}

	if len(items) == 0 {
		it.done = true
		return nil, ErrIteratorDone
	}

	return items, nil
}
