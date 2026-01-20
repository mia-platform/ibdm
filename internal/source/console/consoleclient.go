// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package console

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/mia-platform/ibdm/internal/source"
)

const (
	loggerName = "ibdm:source:console"
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
	log := logger.FromContext(ctx).WithName(loggerName)
	return source.Webhook{
		Method: http.MethodPost,
		Path:   s.c.config.WebhookPath,
		Handler: func(body []byte) error {
			var event event
			if err := json.Unmarshal(body, &event); err != nil {
				log.Error(ErrUnmarshalingEvent.Error(), "body", string(body), "error", err.Error())
				return fmt.Errorf("%w: %s", ErrUnmarshalingEvent, err.Error())
			}

			resource := event.Resource()

			if !event.IsTypeIn(typesToStream) {
				log.Debug("ignoring event with unlisted type", "eventName ", event.EventName, "resource", resource, "name", event.GetName(), "availableTypes", typesToStream)
				return nil
			}

			log.Trace("received event", "type", event.EventName, "resource", resource, "payload", event.Payload, "timestamp", event.UnixEventTimestamp())

			results <- source.Data{
				Type:      resource,
				Operation: event.Operation(),
				Values:    event.Payload,
				Time:      event.UnixEventTimestamp(),
			}
			return nil
		},
	}, nil
}
