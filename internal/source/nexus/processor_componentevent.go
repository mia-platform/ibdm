// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package nexus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/mia-platform/ibdm/internal/source"
)

const (
	// actionCreated is the Nexus webhook action for newly created components.
	actionCreated = "CREATED"
	// actionUpdated is the Nexus webhook action for updated components.
	actionUpdated = "UPDATED"
	// actionDeleted is the Nexus webhook action for deleted components.
	actionDeleted = "DELETED"

	// dockerFormat is the component format value for Docker images.
	dockerFormat = "docker"
)

// componentWebhookPayload is the top-level Nexus webhook payload for rm:repository:component events.
type componentWebhookPayload struct {
	Timestamp      string                  `json:"timestamp"`
	NodeID         string                  `json:"nodeId"`
	Initiator      string                  `json:"initiator"`
	RepositoryName string                  `json:"repositoryName"`
	Action         string                  `json:"action"`
	Component      webhookComponentPayload `json:"component"`
}

// webhookComponentPayload is the component object within a Nexus webhook payload.
type webhookComponentPayload struct {
	ID          string `json:"id"`
	ComponentID string `json:"componentId"`
	Format      string `json:"format"`
	Name        string `json:"name"`
	Group       string `json:"group"`
	Version     string `json:"version"`
}

// componentEventProcessor handles rm:repository:component webhook events.
type componentEventProcessor struct{}

// process implements eventProcessor for rm:repository:component events.
// Only Docker-format components are processed, matching the sync-mode behaviour.
// CREATED and UPDATED actions trigger an upsert of all component assets via REST API enrichment.
// DELETED actions emit a single delete using the webhook payload data.
func (p *componentEventProcessor) process(ctx context.Context, c *client, host string, typesToStream map[string]source.Extra, body []byte) ([]source.Data, error) {
	_, wantDockerImage := typesToStream[dockerImageType]

	// Guard: caller didn't request any known type → skip.
	if !wantDockerImage {
		return nil, nil
	}

	payload, err := parseComponentEvent(body)
	if err != nil {
		return nil, err
	}

	// Only Docker-format components are processed, matching sync-mode behaviour.
	if payload.Component.Format != dockerFormat {
		return nil, nil
	}

	eventTime, err := parseWebhookTimestamp(payload.Timestamp)
	if err != nil {
		return nil, err
	}

	switch payload.Action {
	case actionCreated, actionUpdated:
		return processComponentUpserted(ctx, c, host, payload, eventTime)
	case actionDeleted:
		return processComponentDeleted(host, payload, eventTime)
	default:
		return nil, nil
	}
}

// parseComponentEvent deserializes the raw webhook body into a componentWebhookPayload.
func parseComponentEvent(body []byte) (*componentWebhookPayload, error) {
	var payload componentWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("failed to parse webhook body: %w", err)
	}
	return &payload, nil
}

// processComponentUpserted fetches the full component from the Nexus REST API and
// emits a single source.Data upsert with all assets embedded, mirroring the sync-mode behaviour.
// Returns an error if the API call fails so the event is logged and skipped.
func processComponentUpserted(ctx context.Context, c *client, host string, payload *componentWebhookPayload, eventTime time.Time) ([]source.Data, error) {
	fullComponent, err := c.getComponent(ctx, payload.Component.ComponentID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch component %q from Nexus API: %w", payload.Component.ComponentID, err)
	}

	rawAssets, _ := fullComponent["assets"].([]any)
	assets := make([]map[string]any, 0, len(rawAssets))
	for _, raw := range rawAssets {
		if a, ok := raw.(map[string]any); ok {
			assets = append(assets, a)
		}
	}
	if len(assets) == 0 {
		return nil, nil
	}

	return []source.Data{{
		Type:      dockerImageType,
		Operation: source.DataOperationUpsert,
		Values:    componentWrapper(fullComponent, assets, host),
		Time:      eventTime,
	}}, nil
}

// processComponentDeleted emits a single delete source.Data for the dockerimage type
// using the component identifiers available in the webhook payload (no API call required).
func processComponentDeleted(host string, payload *componentWebhookPayload, eventTime time.Time) ([]source.Data, error) {
	return []source.Data{{
		Type:      dockerImageType,
		Operation: source.DataOperationDelete,
		Values: map[string]any{
			"host":    host,
			"name":    payload.Component.Name,
			"version": payload.Component.Version,
		},
		Time: eventTime,
	}}, nil
}

// parseWebhookTimestamp parses the Nexus webhook timestamp field (RFC 3339) into a time.Time.
// Returns an error if the field is empty or cannot be parsed.
func parseWebhookTimestamp(ts string) (time.Time, error) {
	if ts == "" {
		return time.Time{}, errors.New("component event timestamp is missing from webhook payload")
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return time.Time{}, errors.New("invalid component event timestamp format in webhook payload")
	}
	return t, nil
}
