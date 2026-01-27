// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync/atomic"

	"github.com/mia-platform/ibdm/internal/info"
)

const (
	statusCodeErrorRangeStart = 400
)

var _ ConsoleServiceInterface = &ConsoleService{}

// ConsoleService implements consoleServiceInterface against the Mia-Platform Console API.
type ConsoleService struct {
	config

	client atomic.Pointer[http.Client]
}

func NewConsoleService() (*ConsoleService, error) {
	config, err := loadConfigFromEnv()
	if err != nil {
		return nil, err
	}
	return &ConsoleService{
		config: *config,
	}, nil
}

func (c *ConsoleService) GetProjects(ctx context.Context) ([]map[string]any, error) {
	requestPath := "/projects/"
	return c.doListRequest(ctx, requestPath)
}

func (c *ConsoleService) GetRevisions(ctx context.Context, projectID string) ([]map[string]any, error) {
	requestPath := "/projects/" + projectID + "/revisions"
	return c.doListRequest(ctx, requestPath)
}

func (c *ConsoleService) GetProject(ctx context.Context, projectID string) (map[string]any, error) {
	requestPath := "/projects/" + projectID + "?withTenant=true"
	return c.doRequest(ctx, requestPath)
}

func (c *ConsoleService) GetConfiguration(ctx context.Context, projectID, revisionID string) (map[string]any, error) {
	requestPath := "/projects/" + projectID + "/revisions/" + revisionID + "/configuration"
	return c.doRequest(ctx, requestPath)
}

func (c *ConsoleService) doListRequest(ctx context.Context, requestPath string) ([]map[string]any, error) {
	return doRequest[[]map[string]any](ctx, c, requestPath)
}

func (c *ConsoleService) doRequest(ctx context.Context, requestPath string) (map[string]any, error) {
	return doRequest[map[string]any](ctx, c, requestPath)
}

func doRequest[T []map[string]any | map[string]any](ctx context.Context, c *ConsoleService, requestPath string) (T, error) {
	var nilValue T
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.ConsoleEndpoint+requestPath, nil)
	if err != nil {
		return nilValue, handleError(err)
	}

	request.Header.Set("User-Agent", userAgentString())
	request.Header.Set("Accept", "application/json")

	//nolint:contextcheck // need a new context because it will be used in token requests
	resp, err := c.getClient(context.Background()).Do(request)
	if err != nil {
		return nilValue, handleError(err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusForbidden, http.StatusUnauthorized:
		return nilValue, handleError(errors.New("invalid token or insufficient permissions"))
	case http.StatusNotFound:
		return nilValue, handleError(errors.New("resource not found"))
	case http.StatusNoContent:
		return nilValue, nil
	default:
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nilValue, handleError(err)
		}

		if resp.StatusCode >= statusCodeErrorRangeStart {
			var errResp map[string]any
			if err := json.Unmarshal(body, &errResp); err == nil {
				if msg, ok := errResp["message"].(string); ok {
					return nilValue, handleError(errors.New(msg))
				}
			}
			return nilValue, handleError(errors.New("unexpected error"))
		}

		var respBody T
		if err := json.Unmarshal(body, &respBody); err != nil {
			return nilValue, handleError(err)
		}
		return respBody, nil
	}
}

// userAgentString builds the User-Agent header consumed by the Console API.
func userAgentString() string {
	return info.AppName + "/" + info.Version
}

func (c *ConsoleService) getClient(ctx context.Context) *http.Client {
	client := c.client.Load()
	if client != nil {
		return client
	}

	client = &http.Client{}
	if c.ConsoleJWTServiceAccount {
		client.Transport = newTransportWithJWT(ctx, c.AuthEndpoint, c.PrivateKey, c.PrivateKeyID, c.ClientID)
	}
	client.Transport = newTransport(ctx, c.AuthEndpoint, c.ClientID, c.ClientSecret)
	c.client.Store(client)
	return client
}
