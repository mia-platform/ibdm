// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package nexus

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mia-platform/ibdm/internal/source"
)

const (
	// actionCreated is the Nexus webhook action for newly created components.
	actionCreated = "CREATED"
	// actionDeleted is the Nexus webhook action for deleted components.
	actionDeleted = "DELETED"
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
// CREATED actions trigger an upsert of all component assets via REST API enrichment.
// DELETED actions emit a single delete using the webhook payload data.
func (p *componentEventProcessor) process(ctx context.Context, c *client, host string, typesToStream map[string]source.Extra, body []byte) ([]source.Data, error) {
	// Guard: caller didn't request componentAsset type → skip.
	if _, ok := typesToStream[componentAssetType]; !ok {
		return nil, nil
	}

	payload, err := parseComponentEvent(body)
	if err != nil {
		return nil, err
	}

	switch payload.Action {
	case actionCreated:
		return processComponentCreated(ctx, c, host, payload)
	case actionDeleted:
		return processComponentDeleted(host, payload)
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

// processComponentCreated fetches the full component from the Nexus REST API and
// emits one source.Data upsert per asset. If the API call fails, it falls back
// to the webhook payload data (without asset-level details).
func processComponentCreated(ctx context.Context, c *client, host string, payload *componentWebhookPayload) ([]source.Data, error) {
	fullComponent, err := c.getComponent(ctx, payload.Component.ComponentID)
	if err != nil {
		// Fall back to webhook data if the API call fails.
		fullComponent = webhookPayloadToComponentMap(host, payload)
	}

	assets, _ := fullComponent["assets"].([]any)
	if len(assets) == 0 {
		// No assets available; emit a single upsert with component-level data.
		return []source.Data{
			{
				Type:      componentAssetType,
				Operation: source.DataOperationUpsert,
				Values:    fullComponent,
				Time:      timeSource(),
			},
		}, nil
	}

	result := make([]source.Data, 0, len(assets))
	for _, rawAsset := range assets {
		asset, ok := rawAsset.(map[string]any)
		if !ok {
			continue
		}
		result = append(result, source.Data{
			Type:      componentAssetType,
			Operation: source.DataOperationUpsert,
			Values:    flattenComponentAsset(fullComponent, asset, host),
			Time:      timeSource(),
		})
	}
	return result, nil
}

// processComponentDeleted emits a single delete source.Data using the component
// information available in the webhook payload.
func processComponentDeleted(host string, payload *componentWebhookPayload) ([]source.Data, error) {
	return []source.Data{
		{
			Type:      componentAssetType,
			Operation: source.DataOperationDelete,
			Values: map[string]any{
				"host":       host,
				"id":         payload.Component.ComponentID,
				"repository": payload.RepositoryName,
				"format":     payload.Component.Format,
				"group":      payload.Component.Group,
				"name":       payload.Component.Name,
				"version":    payload.Component.Version,
			},
			Time: timeSource(),
		},
	}, nil
}

// webhookPayloadToComponentMap converts a componentWebhookPayload to the map shape
// expected by flattenComponentAsset, used as fallback when the REST API is unavailable.
func webhookPayloadToComponentMap(host string, payload *componentWebhookPayload) map[string]any {
	return map[string]any{
		"host":       host,
		"id":         payload.Component.ComponentID,
		"repository": payload.RepositoryName,
		"format":     payload.Component.Format,
		"group":      payload.Component.Group,
		"name":       payload.Component.Name,
		"version":    payload.Component.Version,
	}
}
