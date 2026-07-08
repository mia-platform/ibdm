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
	errMultipleAuthMethods        = errors.New("MIA_CATALOG_TOKEN cannot be used with MIA_CATALOG_CLIENT_ID, MIA_CATALOG_CLIENT_SECRET or MIA_CATALOG_PRIVATE_KEY")
	errMissingClientID            = errors.New("MIA_CATALOG_CLIENT_ID is required when MIA_CATALOG_CLIENT_SECRET is set")
	errMissingClientSecret        = errors.New("MIA_CATALOG_CLIENT_SECRET is required when MIA_CATALOG_CLIENT_ID is set")
	errMissingClientIDForPrivKey  = errors.New("MIA_CATALOG_CLIENT_ID is required when MIA_CATALOG_PRIVATE_KEY is set")
	errPrivateKeyWithClientSecret = errors.New("MIA_CATALOG_PRIVATE_KEY cannot be used with MIA_CATALOG_CLIENT_SECRET")
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
	PrivateKey      string `env:"MIA_CATALOG_PRIVATE_KEY"`
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

	if err := destination.validateAuthConfig(); err != nil {
		return nil, handleError(err)
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

// validateAuthConfig ensures that at most one authentication method is configured and that each
// configured method has all of its required environment variables set.
func (d *catalogDestination) validateAuthConfig() error {
	switch {
	case len(d.Token) > 0 && (len(d.ClientID) > 0 || len(d.ClientSecret) > 0 || len(d.PrivateKey) > 0):
		return errMultipleAuthMethods
	case len(d.PrivateKey) > 0 && len(d.ClientSecret) > 0:
		return errPrivateKeyWithClientSecret
	case len(d.PrivateKey) > 0 && len(d.ClientID) == 0:
		return errMissingClientIDForPrivKey
	case len(d.ClientID) > 0 && len(d.ClientSecret) == 0 && len(d.PrivateKey) == 0:
		return errMissingClientSecret
	case len(d.ClientSecret) > 0 && len(d.ClientID) == 0:
		return errMissingClientID
	}

	return nil
}

// SendData implements destination.Sender.
func (d *catalogDestination) SendData(ctx context.Context, data *destination.Data) error {
	return d.handleRequest(ctx, http.MethodPost, data)
}

// DeleteData implements destination.Sender.
func (d *catalogDestination) DeleteData(ctx context.Context, data *destination.Data) error {
	// Catalog API does not have a DELETE endpoint, so we use POST with a specific payload to indicate deletion.
	// DeleteData interface implementation is still provided to allow flexibility in the pipeline and
	// to enable potential future support for a DELETE endpoint without changing the pipeline logic.
	return d.handleRequest(ctx, http.MethodPost, data)
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
	client.Transport = NewTransport(ctx, d.Token, d.AuthEndpoint, d.ClientID, d.ClientSecret, d.PrivateKey)
	d.client.Store(client)
	return client
}
