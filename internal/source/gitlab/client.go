// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package gitlab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/mia-platform/ibdm/internal/info"
)

// ErrIteratorDone is returned by iterator next() methods when all pages have been consumed.
var ErrIteratorDone = errors.New("iterator done")

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

const (
	defaultTimeout = 30 * time.Second
)

// newHTTPClient returns a default HTTP client with the configured timeout.
func newHTTPClient() *http.Client {
	return &http.Client{Timeout: defaultTimeout}
}

// userAgent returns the User-Agent header value used for all GitLab API requests.
func userAgent() string {
	return info.AppName + "/" + info.Version
}

// makeRequest issues a single GET request to the GitLab API and returns the decoded item.
func (c *gitLabClient) makeRequest(ctx context.Context, path, query string) (map[string]any, error) {
	u, err := url.Parse(c.config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}

	u.Path = path

	q, err := url.ParseQuery(query)
	if err != nil {
		return nil, fmt.Errorf("invalid query string: %w", err)
	}

	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("PRIVATE-TOKEN", c.config.Token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent())

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitLab API returned status %d for %s", resp.StatusCode, u.Path)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var items map[string]any
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return items, nil
}

// makePageableRequest issues a single paginated GET request to the GitLab API and returns
// the decoded items together with the total number of pages from the response headers.
func (c *gitLabClient) makePageableRequest(ctx context.Context, path, query string, page int) ([]map[string]any, int, error) {
	u, err := url.Parse(c.config.BaseURL)
	if err != nil {
		return nil, 0, fmt.Errorf("invalid base URL: %w", err)
	}

	u.Path = path

	q, err := url.ParseQuery(query)
	if err != nil {
		return nil, 0, fmt.Errorf("invalid query string: %w", err)
	}

	q.Set("page", strconv.Itoa(page))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("PRIVATE-TOKEN", c.config.Token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent())

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("GitLab API returned status %d for %s", resp.StatusCode, u.Path)
	}

	totalPages, _ := strconv.Atoi(resp.Header.Get("x-total-pages"))

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read response body: %w", err)
	}

	var items []map[string]any
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, 0, fmt.Errorf("failed to decode response: %w", err)
	}

	return items, totalPages, nil
}

// getProjectLanguages fetches the programming language usage breakdown for the given project.
// The API returns a map of language name to usage percentage.
func (c *gitLabClient) getProjectLanguages(ctx context.Context, projectID string) (map[string]any, error) {
	langs, err := c.makeRequest(ctx, "/api/v4/projects/"+projectID+"/languages", "")
	if err != nil {
		return nil, err
	}

	return langs, nil
}

func (c *gitLabClient) getProject(ctx context.Context, projectID int) (map[string]any, error) {
	project, err := c.makeRequest(ctx, "/api/v4/projects/"+strconv.Itoa(projectID), "")
	if err != nil {
		return nil, err
	}

	return project, nil
}
