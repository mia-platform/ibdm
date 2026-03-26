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

var (
	// ErrNotAccessible is returned when a resource cannot be retrieved from the GitLab API.
	// This can be due to insufficient permissions or because the resource does not exist.
	ErrNotAccessible = errors.New("resource not accessible")
)

const (
	defaultTimeout = 30 * time.Second
)

// gitLabClient wraps the HTTP client and source configuration for GitLab API requests.
type gitLabClient struct {
	config sourceConfig
	http   *http.Client
}

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
	q, err := url.ParseQuery(query)
	if err != nil {
		return nil, fmt.Errorf("invalid query string: %w", err)
	}

	_, body, err := c.doRequest(ctx, path, q)
	if err != nil {
		return nil, err
	}

	var item map[string]any
	if err := json.Unmarshal(body, &item); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return item, nil
}

// makeRequestList issues a single GET request to the GitLab API and returns the decoded list of items.
func (c *gitLabClient) makeRequestList(ctx context.Context, path, query string) ([]map[string]any, error) {
	q, err := url.ParseQuery(query)
	if err != nil {
		return nil, fmt.Errorf("invalid query string: %w", err)
	}

	_, body, err := c.doRequest(ctx, path, q)
	if err != nil {
		return nil, err
	}

	var items []map[string]any
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return items, nil
}

// makePageableRequest issues a single paginated GET request to the GitLab API and returns
// the decoded items together with the total number of pages from the response headers.
func (c *gitLabClient) makePageableRequest(ctx context.Context, path string, page int) ([]map[string]any, int, error) {
	q := url.Values{}
	q.Set("per_page", "100")
	q.Set("page", strconv.Itoa(page))

	headers, body, err := c.doRequest(ctx, path, q)
	if err != nil {
		return nil, 0, err
	}

	totalPages, _ := strconv.Atoi(headers.Get("x-total-pages"))

	var items []map[string]any
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, 0, fmt.Errorf("failed to decode response: %w", err)
	}

	return items, totalPages, nil
}

// doRequest builds and fires a GET request to the GitLab API, enforces a 200 status,
// reads and closes the response body, and returns the response headers and body bytes.
func (c *gitLabClient) doRequest(ctx context.Context, path string, q url.Values) (http.Header, []byte, error) {
	u, err := url.Parse(c.config.BaseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid base URL: %w", err)
	}

	u.Path = path
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("PRIVATE-TOKEN", c.config.Token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent())

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var err error
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			err = fmt.Errorf("GitLab API returned status %d for %s: %w", resp.StatusCode, u.Path, ErrNotAccessible)
		} else {
			err = fmt.Errorf("GitLab API returned status %d for %s", resp.StatusCode, u.Path)
		}
		return nil, nil, err
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read response body: %w", err)
	}

	return resp.Header, body, nil
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

func (c *gitLabClient) getPipeline(ctx context.Context, projectID string, pipelineID string) (map[string]any, error) {
	pipeline, err := c.makeRequest(ctx, "/api/v4/projects/"+projectID+"/pipelines/"+pipelineID, "")
	if err != nil {
		return nil, err
	}

	return pipeline, nil
}

// newPageIterator returns a pageIterator for the given API path.
func (c *gitLabClient) newPageIterator(path string) *pageIterator {
	return &pageIterator{c: c, path: path}
}

// listProjects returns an iterator that pages through all GitLab projects.
func (c *gitLabClient) listProjects() iterator {
	return c.newPageIterator("/api/v4/projects")
}

// listGroups returns an iterator that pages through all GitLab groups.
func (c *gitLabClient) listGroups() iterator {
	return c.newPageIterator("/api/v4/groups")
}

// listProjectPipelines returns an iterator that pages through pipelines for the given project.
func (c *gitLabClient) listProjectPipelines(projectID string) iterator {
	return c.newPageIterator("/api/v4/projects/" + projectID + "/pipelines")
}

// listProjectAccessTokens returns an iterator that pages through access tokens for the given project.
func (c *gitLabClient) listProjectAccessTokens(projectID string) iterator {
	return c.newPageIterator("/api/v4/projects/" + projectID + "/access_tokens")
}

// listGroupAccessTokens returns an iterator that pages through access tokens for the given group.
func (c *gitLabClient) listGroupAccessTokens(groupID string) iterator {
	return c.newPageIterator("/api/v4/groups/" + groupID + "/access_tokens")
}
