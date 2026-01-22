// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package console

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync/atomic"

	"github.com/mia-platform/ibdm/internal/info"
)

var _ consoleServiceInterface = &ConsoleService{}

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

// DoRequest implements consoleServiceInterface.
func (c *ConsoleService) DoRequest(ctx context.Context, data any) error {
	return c.handleRequest(ctx, http.MethodPost, data)
}

// handleRequest issues an HTTP call to the Console API using the provided method and payload.
func (c *ConsoleService) handleRequest(ctx context.Context, method string, data any) error {
	body, err := json.Marshal(data)
	if err != nil {
		return handleError(err)
	}

	request, err := http.NewRequestWithContext(ctx, method, c.ConsoleEndpoint, bytes.NewReader(body))
	if err != nil {
		return handleError(err)
	}

	request.Header.Set("User-Agent", userAgentString())
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	//nolint:contextcheck // need a new context because it will be used in token requests
	resp, err := c.getClient(context.Background()).Do(request)
	if err != nil {
		return handleError(err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusForbidden, http.StatusUnauthorized:
		return handleError(errors.New("invalid token or insufficient permissions"))
	case http.StatusNotFound:
		return handleError(errors.New("integration registration not found"))
	case http.StatusNoContent:
		return nil
	default:
		decoder := json.NewDecoder(resp.Body)
		var respBody map[string]any
		if err := decoder.Decode(&respBody); err == nil {
			if message, ok := respBody["message"].(string); ok {
				return handleError(errors.New(message))
			}
		}

		return handleError(errors.New("unexpected error"))
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
	client.Transport = newTransport(ctx, c.AuthEndpoint, c.ClientID, c.ClientSecret)
	c.client.Store(client)
	return client
}
