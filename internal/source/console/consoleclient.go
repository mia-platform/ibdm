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

			eventResource := event.GetResource()

			log.Trace("event type", "event name", event.EventName, "resource", eventResource)
			if !event.IsTypeIn(typesToStream) {
				log.Debug("ignoring event with unlisted type", "type", event.Type, "name", event.GetName())
				return nil
			}

			log.Trace("received event", "type", event.Type)
			log.Trace("received event data", "data", event.Data)

			results <- source.Data{
				Type:      eventResource,
				Operation: source.DataOperationUpsert,
				Values:    event.Data,
			}
			return nil
		},
	}, nil
}
