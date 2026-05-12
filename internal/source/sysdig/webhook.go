// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package sysdig

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/mia-platform/ibdm/internal/source"
)

// GetWebhook implements [source.WebhookSource]. It validates the webhook
// configuration and returns a [source.Webhook] that parses Sysdig pipeline
// failure notifications and dispatches events to the processor registry.
func (s *Source) GetWebhook(ctx context.Context, typesToStream map[string]source.Extra, results chan<- source.Data) (source.Webhook, error) {
	if s.webhookConfig.BaseURL == "" {
		return source.Webhook{}, fmt.Errorf("%w: %w: %s",
			ErrSysdigSource, ErrMissingEnvVariable, "SYSDIG_BASE_URL")
	}
	if s.webhookConfig.BearerToken == "" {
		return source.Webhook{}, fmt.Errorf("%w: %w: %s",
			ErrSysdigSource, ErrMissingEnvVariable, "SYSDIG_BEARER_TOKEN")
	}

	return source.Webhook{
		Method: http.MethodPost,
		Path:   s.webhookConfig.WebhookPath,
		Handler: func(ctx context.Context, _ http.Header, body []byte) error {
			log := logger.FromContext(ctx).WithName(loggerName)

			eventType, err := resolveEventType(body)
			if err != nil {
				log.Error("failed to resolve event type from webhook payload", "error", err.Error())
				return fmt.Errorf("%w: %w", ErrSysdigSource, err)
			}

			processor, ok := eventProcessors[eventType]
			if !ok {
				log.Debug("ignoring unsupported event", "eventType", eventType)
				return nil
			}

			go func() {
				data, err := processor.process(ctx, s.vulnClient, typesToStream, body)
				if err != nil {
					log.Error("error processing webhook event", "event", eventType, "error", err.Error())
					return
				}

				for _, d := range data {
					results <- d
				}
			}()

			return nil
		},
	}, nil
}

// resolveEventType extracts the event type from the webhook payload body.
// It checks event.id first (higher priority), then event.eventData.name.
// Returns the matched value, or the event.id if neither matches a known
// processor (the dispatcher will handle the unknown-event case).
func resolveEventType(body []byte) (string, error) {
	var envelope struct {
		Event struct {
			ID        string `json:"id"`
			EventData struct {
				Name string `json:"name"`
			} `json:"eventData"`
		} `json:"event"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return "", fmt.Errorf("failed to parse webhook body: %w", err)
	}

	// event.id has higher priority
	if _, ok := eventProcessors[envelope.Event.ID]; ok {
		return envelope.Event.ID, nil
	}

	// fall back to event.eventData.name
	if _, ok := eventProcessors[envelope.Event.EventData.Name]; ok {
		return envelope.Event.EventData.Name, nil
	}

	// Return the id for logging purposes; the dispatcher will skip it
	return envelope.Event.ID, nil
}
