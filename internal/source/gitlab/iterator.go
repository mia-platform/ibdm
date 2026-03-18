// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package gitlab

import (
	"context"
	"errors"
	"fmt"
)

var (
	// ErrIteratorDone is returned by iterator next() methods when all pages have been consumed.
	ErrIteratorDone = errors.New("iterator done")
)

// iterator defines the contract for paginated GitLab API iterators.
type iterator interface {
	next(ctx context.Context) ([]map[string]any, error)
}

// pageIterator pages through a GitLab list endpoint. The path is fixed at
// construction time and can represent either a top-level list (e.g. /api/v4/projects)
// or a resource scoped to a parent item (e.g. /api/v4/projects/42/pipelines).
type pageIterator struct {
	c          *gitLabClient
	path       string
	currPage   int
	totalPages int
	done       bool
}

// newPageIterator returns a pageIterator for the given API path.
func (c *gitLabClient) newPageIterator(path string) *pageIterator {
	return &pageIterator{c: c, path: path}
}

// newProjectsIterator returns an iterator ready to stream all projects.
func (c *gitLabClient) newProjectsIterator() iterator {
	return c.newPageIterator("/api/v4/projects")
}

// newGroupsIterator returns an iterator ready to stream all groups.
func (c *gitLabClient) newGroupsIterator() iterator {
	return c.newPageIterator("/api/v4/groups")
}

// newProjectResourcesIterator returns an iterator for the given resource
// scoped to a project. The resource must be one of the package-level resource constants.
func (c *gitLabClient) newProjectResourcesIterator(resource, projectID string) (iterator, error) {
	var path string

	switch resource {
	case pipelineResource:
		path = fmt.Sprintf("/api/v4/projects/%s/pipelines", projectID)
	case accessTokenResource:
		path = fmt.Sprintf("/api/v4/projects/%s/access_tokens", projectID)
	default:
		return nil, fmt.Errorf("unknown project resource: %s", resource)
	}

	return c.newPageIterator(path), nil
}

// newGroupResourcesIterator returns an iterator for the given resource
// scoped to a group. The resource must be one of the package-level resource constants.
func (c *gitLabClient) newGroupResourcesIterator(resource, groupID string) (iterator, error) {
	var path string

	switch resource {
	case accessTokenResource:
		path = fmt.Sprintf("/api/v4/groups/%s/access_tokens", groupID)
	default:
		return nil, fmt.Errorf("unknown group resource: %s", resource)
	}

	return c.newPageIterator(path), nil
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
