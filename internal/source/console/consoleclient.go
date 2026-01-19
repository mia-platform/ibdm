// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package console

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/mia-platform/ibdm/internal/source"
)

var (
	ErrSourceCreation    = errors.New("console source creation error")
	ErrUnmarshalingEvent = errors.New("error unmarshaling console event")
)

func NewSource() (*Source, error) {
	consoleClient, err := newConsoleClient()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrSourceCreation, err.Error())
	}

	return &Source{
		c: consoleClient,
	}, nil
}

func newConsoleClient() (*consoleClient, error) {
	config, err := loadConfigFromEnv()
	if err != nil {
		return nil, err
	}

	return &consoleClient{
		config: *config,
	}, nil
}

func (s *Source) GetWebhook(ctx context.Context, typesToStream []string, results chan<- source.Data) (source.Webhook, error) {
	return source.Webhook{
		Method: http.MethodPost,
		Path:   s.c.config.WebhookPath,
		Handler: func(body []byte) error {
			var event event
			if err := json.Unmarshal(body, &event); err != nil {
				return fmt.Errorf("%w: %s", ErrUnmarshalingEvent, err.Error())
			}

			if !slices.Contains(typesToStream, event.Type) {
				return nil
			}

			results <- source.Data{
				Type:      event.Type,
				Operation: source.DataOperationUpsert,
				Values:    event.Data,
			}
			return nil
		},
	}, nil
}
