// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/caarlos0/env/v11"

	"github.com/mia-platform/ibdm/internal/destination"
	"github.com/mia-platform/ibdm/internal/info"
)

var _ destination.Sender = &catalogDestination{}

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

// catalogDestination implements destination.Sender for sending and deleting data
// in the Mia-Platform Catalog.
type catalogDestination struct {
	CatalogEndpoint string `env:"MIA_CATALOG_ENDPOINT,required"`
	Token           string `env:"MIA_CATALOG_TOKEN"`
}

// NewDestination returns a new destination.Sender configured to connect to the
// Mia-Platform Catalog. Its configuration is read from environment variables.
func NewDestination() (destination.Sender, error) {
	destination := new(catalogDestination)
	if err := env.Parse(destination); err != nil {
		return nil, handleError(err)
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

// handleRequest sends an HTTP request to the Catalog API with the given method and data.
// It will marshal the data into JSON and set the appropriate headers.
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
	request.Header.Set("Authorization", "Bearer "+d.Token)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		return handleError(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		decoder := json.NewDecoder(resp.Body)
		var respBody map[string]any
		if err := decoder.Decode(&respBody); err == nil {
			if message, ok := respBody["message"].(string); ok {
				return handleError(errors.New(message))
			}
		}

		return handleError(errors.New("unexpected error"))
	}

	return nil
}

// userAgentString returns the User-Agent string to be used in HTTP requests.
func userAgentString() string {
	return info.AppName + "/" + info.Version
}

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
