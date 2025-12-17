// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync/atomic"

	"github.com/caarlos0/env/v11"

	"github.com/mia-platform/ibdm/internal/destination"
	"github.com/mia-platform/ibdm/internal/info"
)

var (
	errMultipleAuthMethods = errors.New("MIA_CATALOG_TOKEN cannot be used with MIA_CATALOG_CLIENT_ID or MIA_CATALOG_CLIENT_SECRET")
	errMissingClientID     = errors.New("MIA_CATALOG_CLIENT_ID is required when MIA_CATALOG_CLIENT_SECRET is set")
	errMissingClientSecret = errors.New("MIA_CATALOG_CLIENT_SECRET is required when MIA_CATALOG_CLIENT_ID is set")
)

var _ destination.Sender = &catalogDestination{}

// CatalogError wraps lower-level errors produced by the Catalog destination.
type CatalogError struct {
	err error
}

func (e *CatalogError) Error() string {
	return "catalog: " + e.err.Error()
}

func (e *CatalogError) Unwrap() error {
	return e.err
}

func (e *CatalogError) Is(target error) bool {
	cre, ok := target.(*CatalogError)
	if !ok {
		return false
	}

	return e.err.Error() == cre.err.Error()
}

// catalogDestination implements destination.Sender against the Mia-Platform Catalog API.
type catalogDestination struct {
	CatalogEndpoint string `env:"MIA_CATALOG_ENDPOINT,required"`
	Token           string `env:"MIA_CATALOG_TOKEN"`
	ClientID        string `env:"MIA_CATALOG_CLIENT_ID"`
	ClientSecret    string `env:"MIA_CATALOG_CLIENT_SECRET"`
	AuthEndpoint    string `env:"MIA_CATALOG_AUTH_ENDPOINT"`

	client atomic.Pointer[http.Client]
}

// NewDestination loads configuration from environment variables and returns a Catalog-backed destination.Sender.
func NewDestination() (destination.Sender, error) {
	destination := new(catalogDestination)
	if err := env.Parse(destination); err != nil {
		return nil, handleError(err)
	}

	endpointURL, err := url.Parse(destination.CatalogEndpoint)
	if err != nil {
		return nil, handleError(fmt.Errorf("invalid MIA_CATALOG_ENDPOINT: %w", err))
	}

	switch {
	case len(destination.Token) > 0 && (len(destination.ClientID) > 0 || len(destination.ClientSecret) > 0):
		return nil, handleError(errMultipleAuthMethods)
	case len(destination.ClientID) > 0 && len(destination.ClientSecret) == 0:
		return nil, handleError(errMissingClientSecret)
	case len(destination.ClientSecret) > 0 && len(destination.ClientID) == 0:
		return nil, handleError(errMissingClientID)
	}

	if len(destination.AuthEndpoint) == 0 {
		endpointURL.Path = "/oauth/token"
		destination.AuthEndpoint = endpointURL.String()
	} else {
		_, err := url.Parse(destination.AuthEndpoint)
		if err != nil {
			return nil, handleError(fmt.Errorf("invalid MIA_CATALOG_AUTH_ENDPOINT: %w", err))
		}
	}

	return destination, nil
}

// SendData implements destination.Sender.
func (d *catalogDestination) SendData(ctx context.Context, data *destination.Data) error {
	return d.handleRequest(ctx, http.MethodPost, data)
}

// DeleteData implements destination.Sender.
func (d *catalogDestination) DeleteData(ctx context.Context, data *destination.Data) error {
	return d.handleRequest(ctx, http.MethodDelete, data)
}

// handleRequest issues an HTTP call to the Catalog API using the provided method and payload.
func (d *catalogDestination) handleRequest(ctx context.Context, method string, data *destination.Data) error {
	body, err := json.Marshal(data)
	if err != nil {
		return handleError(err)
	}

	request, err := http.NewRequestWithContext(ctx, method, d.CatalogEndpoint, bytes.NewReader(body))
	if err != nil {
		return handleError(err)
	}

	request.Header.Set("User-Agent", userAgentString())
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	//nolint:contextcheck // need a new context because it will be used in token requests
	resp, err := d.getClient(context.Background()).Do(request)
	if err != nil {
		return handleError(err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusForbidden, http.StatusUnauthorized:
		return handleError(errors.New("invalid token or insufficient permissions"))
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

// userAgentString builds the User-Agent header consumed by the Catalog API.
func userAgentString() string {
	return info.AppName + "/" + info.Version
}

// handleError normalizes errors emitted by the Catalog destination.
func handleError(err error) error {
	var parseErr env.AggregateError
	if errors.As(err, &parseErr) {
		err = parseErr.Errors[0]
	}

	if errors.Is(err, context.Canceled) {
		return nil
	}

	return &CatalogError{
		err: err,
	}
}

func (d *catalogDestination) getClient(ctx context.Context) *http.Client {
	client := d.client.Load()
	if client != nil {
		return client
	}

	client = &http.Client{}
	client.Transport = NewTransport(ctx, d.Token, d.AuthEndpoint, d.ClientID, d.ClientSecret)
	d.client.Store(client)
	return client
}
