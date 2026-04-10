// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package bitbucket

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// client wraps an HTTP client with Bitbucket-specific configuration.
type client struct {
	baseURL     string
	accessToken string // set when using Bearer token auth
	apiUsername string // set when using Basic auth
	apiToken    string // set when using Basic auth
	httpClient  *http.Client
}

// doRequest executes an authenticated GET request against the Bitbucket REST API
// and returns the raw response. The caller is responsible for closing the body.
// The rawURL parameter is the full URL including any query parameters.
func (c *client) doRequest(ctx context.Context, rawURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if c.accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.accessToken)
	} else {
		req.SetBasicAuth(c.apiUsername, c.apiToken)
	}
	req.Header.Set("Accept", "application/json")

	return c.httpClient.Do(req)
}

// listWorkspaces returns an iterator that pages through all workspaces
// accessible to the authenticated user.
func (c *client) listWorkspaces() iterator {
	initialURL := c.baseURL + "/2.0/user/workspaces?pagelen=100"
	return &pageIterator{client: c, nextURL: initialURL}
}

// listRepositories returns an iterator that pages through all repositories
// in the given workspace.
func (c *client) listRepositories(workspaceSlug string) iterator {
	initialURL := fmt.Sprintf("%s/2.0/repositories/%s?pagelen=100",
		c.baseURL, url.PathEscape(workspaceSlug))
	return &pageIterator{client: c, nextURL: initialURL}
}

// listPipelines returns an iterator that pages through all pipelines
// for the given repository.
func (c *client) listPipelines(workspaceSlug, repoSlug string) iterator {
	// No pagelen is specified intentionally: the official Bitbucket documentation is ambiguous about the maximum
	// supported pagelen for this resource, and the actual behaviour is best determined by inspecting real API responses after deployment.
	initialURL := fmt.Sprintf("%s/2.0/repositories/%s/%s/pipelines",
		c.baseURL, url.PathEscape(workspaceSlug), url.PathEscape(repoSlug))
	return &pageIterator{client: c, nextURL: initialURL}
}

// getRepository fetches a single repository by workspace slug and repo slug.
func (c *client) getRepository(ctx context.Context, workspaceSlug, repoSlug string) (map[string]any, error) {
	rawURL := fmt.Sprintf("%s/2.0/repositories/%s/%s",
		c.baseURL, url.PathEscape(workspaceSlug), url.PathEscape(repoSlug))

	resp, err := c.doRequest(ctx, rawURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch repository: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var repo map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&repo); err != nil {
		return nil, fmt.Errorf("failed to decode repository response: %w", err)
	}

	return repo, nil
}
