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

// itemIterator pages through a top-level GitLab list endpoint (e.g. /api/v4/projects
// or /api/v4/groups). The path is fixed at construction time.
type itemIterator struct {
	c          *gitLabClient
	path       string
	currPage   int
	totalPages int
	done       bool
}

// itemResourcesIterator pages through a resource scoped to a specific parent item
// (e.g. /api/v4/projects/42/pipelines). The path is resolved and fixed at construction time.
type itemResourcesIterator struct {
	c          *gitLabClient
	path       string
	currPage   int
	totalPages int
	done       bool
}

// newItemIterator returns an itemIterator for the given API path.
func (c *gitLabClient) newItemIterator(path string) *itemIterator {
	return &itemIterator{c: c, path: path}
}

// newProjectsIterator returns an itemIterator ready to stream all projects.
func (c *gitLabClient) newProjectsIterator() *itemIterator {
	return c.newItemIterator("/api/v4/projects")
}

// newGroupsIterator returns an itemIterator ready to stream all groups.
func (c *gitLabClient) newGroupsIterator() *itemIterator {
	return c.newItemIterator("/api/v4/groups")
}

// newItemResourcesIterator returns an itemResourcesIterator for the given API path.
func (c *gitLabClient) newItemResourcesIterator(path string) *itemResourcesIterator {
	return &itemResourcesIterator{c: c, path: path}
}

// newProjectResourcesIterator returns an itemResourcesIterator for the given resource
// scoped to a project. The resource must be one of the package-level resource constants.
func (c *gitLabClient) newProjectResourcesIterator(resource, projectID string) (*itemResourcesIterator, error) {
	var path string

	switch resource {
	case pipelineResource:
		path = fmt.Sprintf("/api/v4/projects/%s/pipelines", projectID)
	case accessTokenResource:
		path = fmt.Sprintf("/api/v4/projects/%s/access_tokens", projectID)
	default:
		return nil, fmt.Errorf("unknown project resource: %s", resource)
	}

	return c.newItemResourcesIterator(path), nil
}

// newGroupResourcesIterator returns an itemResourcesIterator for the given resource
// scoped to a group. The resource must be one of the package-level resource constants.
func (c *gitLabClient) newGroupResourcesIterator(resource, groupID string) (*itemResourcesIterator, error) {
	var path string

	switch resource {
	case accessTokenResource:
		path = fmt.Sprintf("/api/v4/groups/%s/access_tokens", groupID)
	default:
		return nil, fmt.Errorf("unknown group resource: %s", resource)
	}

	return c.newItemResourcesIterator(path), nil
}

// next fetches the next page of items. Returns ErrIteratorDone when all pages
// have been consumed. The caller never receives an empty slice.
func (it *itemIterator) next(ctx context.Context) ([]map[string]any, error) {
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

// next fetches the next page of the scoped resource. Returns ErrIteratorDone
// when all pages have been consumed. The caller never receives an empty slice.
func (it *itemResourcesIterator) next(ctx context.Context) ([]map[string]any, error) {
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
