// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/mia-platform/ibdm/internal/info"
)

const (
	defaultTimeout = 30 * time.Second
)

var (
	// TODO: remove maxPagesLimit when Gitlab source is stable.
	// maxPagesLimit is the maximum number of pages fetched in a single listing call.
	maxPagesLimit = 1
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
// the decoded items together with the current page and total pages from the response headers.
func (c *gitLabClient) makePageableRequest(ctx context.Context, path, query string, page int) ([]map[string]any, int, int, error) {
	u, err := url.Parse(c.config.BaseURL)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("invalid base URL: %w", err)
	}

	u.Path = path

	q, err := url.ParseQuery(query)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("invalid query string: %w", err)
	}

	q.Set("page", strconv.Itoa(page))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("PRIVATE-TOKEN", c.config.Token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent())

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, 0, 0, fmt.Errorf("GitLab API returned status %d for %s", resp.StatusCode, u.Path)
	}

	currPage, _ := strconv.Atoi(resp.Header.Get("x-page"))
	totalPages, _ := strconv.Atoi(resp.Header.Get("x-total-pages"))

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("failed to read response body: %w", err)
	}

	var items []map[string]any
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, 0, 0, fmt.Errorf("failed to decode response: %w", err)
	}

	return items, currPage, totalPages, nil
}

// listAllPages fetches all pages of a GitLab API endpoint up to maxPagesLimit,
// returning the aggregated results.
func (c *gitLabClient) listAllPages(ctx context.Context, path, query string) ([]map[string]any, error) {
	var all []map[string]any

	for page := 1; page <= maxPagesLimit; page++ {
		items, _, totalPages, err := c.makePageableRequest(ctx, path, query, page)
		if err != nil {
			return nil, err
		}

		all = append(all, items...)

		if totalPages <= 0 || page >= totalPages {
			break
		}
	}

	return all, nil
}

func (c *gitLabClient) getProject(ctx context.Context, projectID int) (map[string]any, error) {
	project, err := c.makeRequest(ctx, "/api/v4/projects/"+strconv.Itoa(projectID), "")
	if err != nil {
		return nil, err
	}

	return project, nil
}

// listProjects returns all accessible GitLab projects, crawling all available pages.
func (c *gitLabClient) listProjects(ctx context.Context) ([]map[string]any, error) {
	return c.listAllPages(ctx, "/api/v4/projects", "per_page=100")
}

// listPipelines returns all pipelines for the given project ID, crawling all available pages.
func (c *gitLabClient) listPipelines(ctx context.Context, projectID string) ([]map[string]any, error) {
	return c.listAllPages(ctx, "/api/v4/projects/"+projectID+"/pipelines", "per_page=100")
}

// // listMergeRequests returns all merge requests for the given project ID, crawling all available pages.
// // Reserved for future sync extension.
// func (c *gitLabClient) listMergeRequests(ctx context.Context, projectID string) ([]map[string]any, error) {
// 	return c.listAllPages(ctx, "/api/v4/projects/"+projectID+"/merge_requests", "state=all&per_page=100")
// }

// // listReleases returns all releases for the given project ID, crawling all available pages.
// // Reserved for future sync extension.
// func (c *gitLabClient) listReleases(ctx context.Context, projectID string) ([]map[string]any, error) {
// 	return c.listAllPages(ctx, "/api/v4/projects/"+projectID+"/releases", "per_page=100")
// }
