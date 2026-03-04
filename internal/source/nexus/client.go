// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package nexus

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

const (
	// apiBasePath is the fixed base path for the Nexus REST API.
	apiBasePath = "/service/rest"

	// maxErrorBodySize limits how many bytes we read from error response bodies
	// to avoid unbounded memory allocation on unexpectedly large payloads.
	maxErrorBodySize = 1024
)

// client wraps an HTTP client with Nexus-specific configuration.
type client struct {
	baseURL       *url.URL
	tokenName     string
	tokenPasscode string

	httpClient *http.Client
}

// newClient creates a client from the given config.
func newClient(cfg config) (*client, error) {
	u, err := url.Parse(cfg.URLSchema + "://" + cfg.URLHost)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid NEXUS_URL_SCHEMA or NEXUS_URL_HOST: %w", ErrInvalidEnvVariable, err)
	}

	return &client{
		baseURL:       u,
		tokenName:     cfg.TokenName,
		tokenPasscode: cfg.TokenPasscode,
		httpClient: &http.Client{
			Timeout: cfg.HTTPTimeout,
		},
	}, nil
}

// doRequest executes an authenticated GET request against the Nexus REST API
// and returns the response. The caller is responsible for closing the body.
func (c *client) doRequest(ctx context.Context, path string, queryParams url.Values) (*http.Response, error) {
	u := c.baseURL.JoinPath(apiBasePath, path)
	if queryParams != nil {
		u.RawQuery = queryParams.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}

	req.SetBasicAuth(c.tokenName, c.tokenPasscode)
	req.Header.Set("Accept", "application/json")

	return c.httpClient.Do(req)
}

// repositoriesResponse represents a single RepositoryXO from the Nexus API.
type repositoriesResponse = map[string]any

// componentsPageResponse represents the PageComponentXO response from the Nexus API.
type componentsPageResponse struct {
	Items             []map[string]any `json:"items"`
	ContinuationToken *string          `json:"continuationToken"`
}

// listRepositories fetches all repositories from the Nexus instance.
func (c *client) listRepositories(ctx context.Context) ([]repositoriesResponse, error) {
	resp, err := c.doRequest(ctx, "/v1/repositories", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, readErrorResponse(resp)
	}

	var repos []repositoriesResponse
	if err := json.NewDecoder(resp.Body).Decode(&repos); err != nil {
		return nil, fmt.Errorf("failed to decode repositories response: %w", err)
	}

	return repos, nil
}

// getRepository fetches a single repository by name.
func (c *client) getRepository(ctx context.Context, name string) (repositoriesResponse, error) {
	path := "/v1/repositories/" + url.PathEscape(name)

	resp, err := c.doRequest(ctx, path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, readErrorResponse(resp)
	}

	var repo repositoriesResponse
	if err := json.NewDecoder(resp.Body).Decode(&repo); err != nil {
		return nil, fmt.Errorf("failed to decode repository response: %w", err)
	}

	return repo, nil
}

// listComponentsPage fetches a single page of components for the given repository.
// Pass an empty continuationToken for the first page.
func (c *client) listComponentsPage(ctx context.Context, repository, continuationToken string) (*componentsPageResponse, error) {
	params := url.Values{}
	params.Set("repository", repository)
	if continuationToken != "" {
		params.Set("continuationToken", continuationToken)
	}

	resp, err := c.doRequest(ctx, "/v1/components", params)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, readErrorResponse(resp)
	}

	var page componentsPageResponse
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return nil, fmt.Errorf("failed to decode components response: %w", err)
	}

	return &page, nil
}

// readErrorResponse constructs an error from a non-2xx HTTP response,
// reading at most maxErrorBodySize bytes from the body.
func readErrorResponse(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodySize))
	return fmt.Errorf("nexus API returned status %d: %s", resp.StatusCode, string(body))
}
