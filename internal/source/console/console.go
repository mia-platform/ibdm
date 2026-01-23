// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package console

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"

	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/mia-platform/ibdm/internal/source"
	"github.com/mia-platform/ibdm/internal/source/console/service"
)

const (
	loggerName = "ibdm:source:console"
)

var (
	ErrSourceCreation       = errors.New("console source creation error")
	ErrUnmarshalingEvent    = errors.New("error unmarshaling console event")
	ErrEventChainProcessing = errors.New("error in event processing chain")
)

// Source wires Console clients to satisfy source interfaces.
type webhookClient struct {
	config webhookConfig
}

var _ source.WebhookSource = &Source{}

type Source struct {
	c  *webhookClient
	cs service.ConsoleServiceInterface
}

func NewSource() (*Source, error) {
	consoleClient, err := newConsoleClient()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrSourceCreation, err.Error())
	}

	consoleService, err := service.NewConsoleService()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrSourceCreation, err.Error())
	}

	return &Source{
		c:  consoleClient,
		cs: consoleService,
	}, nil
}

func newConsoleClient() (*webhookClient, error) {
	config, err := loadConfigFromEnv()
	if err != nil {
		return nil, err
	}

	return &webhookClient{
		config: *config,
	}, nil
}

func (s *Source) GetWebhook(ctx context.Context, typesToStream map[string]source.Extra, results chan<- source.Data) (source.Webhook, error) {
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

			if !event.IsTypeIn(slices.Sorted(maps.Keys(typesToStream))) {
				log.Debug("ignoring event with unlisted type", "eventName ", event.EventName, "resource", event.GetResource(), "name", event.GetName())
				return nil
			}

			log.Trace("received event", "type", event.EventName, "resource", event.GetResource(), "payload", event.Payload, "timestamp", event.UnixEventTimestamp())

			if err := doChain(ctx, event, results, s.cs); err != nil {
				log.Error("error processing event chain", "error", err.Error())
				return err
			}
			return nil
		},
	}, nil
}

func doChain(ctx context.Context, event event, channel chan<- source.Data, cs service.ConsoleServiceInterface) error {
	var data []source.Data
	var err error
	switch event.GetResource() {
	case "configuration":
		data, err = configurationEventChain(ctx, event, cs)
	case "project":
		data = defaultEventChain(event)
	default:
		data = defaultEventChain(event)
	}
	if err != nil {
		return fmt.Errorf("%w: %s", ErrEventChainProcessing, err.Error())
	}
	for _, d := range data {
		channel <- d
	}
	return nil
}

func defaultEventChain(event event) []source.Data {
	return []source.Data{
		{
			Type:      event.GetResource(),
			Operation: event.Operation(),
			Values:    event.Payload,
			Time:      event.UnixEventTimestamp(),
		},
	}
}

func configurationEventChain(ctx context.Context, event event, cs service.ConsoleServiceInterface) ([]source.Data, error) {
	var projectID, resourceID string
	var ok bool
	if event.Payload == nil {
		return nil, errors.New("configuration event payload is nil")
	}
	if projectID, ok = event.Payload["projectId"].(string); !ok {
		return nil, errors.New("configuration event payload missing projectId")
	}
	if resourceID, ok = event.Payload["resourceId"].(string); !ok {
		return nil, errors.New("configuration event payload missing resourceId")
	}

	configuration, err := cs.GetRevision(ctx, projectID, resourceID)
	if err != nil {
		return nil, err
	}

	values := map[string]any{
		"event":         event.Payload,
		"configuration": configuration,
	}

	return []source.Data{
		{
			Type:      event.GetResource(),
			Operation: event.Operation(),
			Values:    values,
			Time:      event.UnixEventTimestamp(),
		},
	}, nil
}
