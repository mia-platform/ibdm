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

// projectsIterator pages through the top-level GitLab projects list.
type projectsIterator struct {
	c          *gitLabClient
	currPage   int
	totalPages int
	done       bool
}

// projectResourcesIterator pages through a resource scoped to a specific project
// (e.g. pipelines). resource must be one of the package-level resource constants.
type projectResourcesIterator struct {
	c          *gitLabClient
	resource   string
	projectID  string
	currPage   int
	totalPages int
	done       bool
}

// newProjectsIterator returns a projectsIterator ready to stream all projects.
func (c *gitLabClient) newProjectsIterator() *projectsIterator {
	return &projectsIterator{c: c}
}

// newProjectResourcesIterator returns a projectResourcesIterator for the given resource
// and project. resource must be one of the package-level resource constants (e.g. pipelineResource).
func (c *gitLabClient) newProjectResourcesIterator(resource, projectID string) *projectResourcesIterator {
	return &projectResourcesIterator{c: c, resource: resource, projectID: projectID}
}

// next fetches the next page of projects. Returns ErrIteratorDone when all pages
// have been consumed. The caller never receives an empty slice.
func (it *projectsIterator) next(ctx context.Context) ([]map[string]any, error) {
	if it.done {
		return nil, ErrIteratorDone
	}

	it.currPage++

	items, totalPages, err := it.c.makePageableRequest(ctx, "/api/v4/projects", "per_page=100", it.currPage)
	if err != nil {
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

// next fetches the next page of the project-scoped resource. Returns ErrIteratorDone
// when all pages have been consumed. The caller never receives an empty slice.
func (it *projectResourcesIterator) next(ctx context.Context) ([]map[string]any, error) {
	if it.done {
		return nil, ErrIteratorDone
	}

	var path, query string

	switch it.resource {
	case pipelineResource:
		path = fmt.Sprintf("/api/v4/projects/%s/pipelines", it.projectID)
		query = "per_page=100"
	default:
		return nil, fmt.Errorf("unknown resource: %s", it.resource)
	}

	it.currPage++

	items, totalPages, err := it.c.makePageableRequest(ctx, path, query, it.currPage)
	if err != nil {
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
