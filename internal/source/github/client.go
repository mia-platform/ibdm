// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package github

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

const (
	// userAgent is sent as the User-Agent header on all GitHub API requests.
	// GitHub may block requests that omit a User-Agent header.
	userAgent = "ibdm-github-source"
)

// client wraps an HTTP client with GitHub-specific configuration.
type client struct {
	baseURL    string
	org        string
	token      string
	pageSize   int
	httpClient *http.Client
}

// doRequest executes an authenticated GET request against the GitHub REST API
// and returns the raw response. The caller is responsible for closing the body.
func (c *client) doRequest(ctx context.Context, path, apiVersion string, page int) (*http.Response, error) {
	u, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, fmt.Errorf("failed to build request URL: %w", err)
	}

	q := u.Query()
	q.Set("per_page", strconv.Itoa(c.pageSize))
	q.Set("page", strconv.Itoa(page))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	req.Header.Set("User-Agent", userAgent)

	return c.httpClient.Do(req)
}

// newPageIterator returns a new page iterator for the given path and API version.
func (c *client) newPageIterator(path, apiVersion string) iterator {
	return &pageIterator{
		client:     c,
		path:       path,
		apiVersion: apiVersion,
	}
}

// listRepositories returns an iterator that pages through all organization repositories.
func (c *client) listRepositories(apiVersion string) iterator {
	return c.newPageIterator("/orgs/"+url.PathEscape(c.org)+"/repos?type=all", apiVersion)
}

// listWorkflowRuns returns an iterator that pages through all workflow runs
// for the given repository identified by owner and repo name.
func (c *client) listWorkflowRuns(owner, repo, apiVersion string) iterator {
	return &wrappedPageIterator{
		client:      c,
		path:        "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/actions/runs",
		apiVersion:  apiVersion,
		responseKey: "workflow_runs",
	}
}
