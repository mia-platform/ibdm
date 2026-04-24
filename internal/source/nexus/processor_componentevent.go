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
// CREATED actions trigger an upsert of all component assets via REST API enrichment.
// DELETED actions emit a single delete using the webhook payload data.
func (p *componentEventProcessor) process(ctx context.Context, c *client, host string, typesToStream map[string]source.Extra, body []byte) ([]source.Data, error) {
	_, wantComponentAsset := typesToStream[componentAssetType]
	_, wantDockerImage := typesToStream[dockerImageType]

	// Guard: caller didn't request any known type → skip.
	if !wantComponentAsset && !wantDockerImage {
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
	case actionCreated:
		return processComponentCreated(ctx, c, host, typesToStream, payload, eventTime)
	case actionDeleted:
		return processComponentDeleted(host, typesToStream, payload, eventTime)
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
// emits one or more source.Data upserts, mirroring the sync-mode fan-out:
//   - one dockerimage entry (if requested)
//   - one componentasset entry per asset (if requested)
//
// Returns an error if the API call fails so the event is logged and skipped.
func processComponentCreated(ctx context.Context, c *client, host string, typesToStream map[string]source.Extra, payload *componentWebhookPayload, eventTime time.Time) ([]source.Data, error) {
	_, wantComponentAsset := typesToStream[componentAssetType]
	_, wantDockerImage := typesToStream[dockerImageType]

	fullComponent, err := c.getComponent(ctx, payload.Component.ComponentID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch component %q from Nexus API: %w", payload.Component.ComponentID, err)
	}

	var result []source.Data

	if wantDockerImage {
		result = append(result, source.Data{
			Type:      dockerImageType,
			Operation: source.DataOperationUpsert,
			Values: map[string]any{
				"host":    host,
				"name":    fullComponent["name"],
				"version": fullComponent["version"],
			},
			Time: eventTime,
		})
	}

	if wantComponentAsset {
		assets, _ := fullComponent["assets"].([]any)
		if len(assets) == 0 {
			// No assets available; emit a single upsert with component-level data.
			result = append(result, source.Data{
				Type:      componentAssetType,
				Operation: source.DataOperationUpsert,
				Values:    fullComponent,
				Time:      eventTime,
			})
		} else {
			for _, rawAsset := range assets {
				asset, ok := rawAsset.(map[string]any)
				if !ok {
					continue
				}
				result = append(result, source.Data{
					Type:      componentAssetType,
					Operation: source.DataOperationUpsert,
					Values:    flattenComponentAsset(fullComponent, asset, host),
					Time:      eventTime,
				})
			}
		}
	}

	return result, nil
}

// processComponentDeleted emits delete source.Data entries using the component
// information available in the webhook payload, mirroring the sync-mode types.
func processComponentDeleted(host string, typesToStream map[string]source.Extra, payload *componentWebhookPayload, eventTime time.Time) ([]source.Data, error) {
	_, wantComponentAsset := typesToStream[componentAssetType]
	_, wantDockerImage := typesToStream[dockerImageType]

	var result []source.Data

	if wantDockerImage {
		result = append(result, source.Data{
			Type:      dockerImageType,
			Operation: source.DataOperationDelete,
			Values: map[string]any{
				"host":    host,
				"name":    payload.Component.Name,
				"version": payload.Component.Version,
			},
			Time: eventTime,
		})
	}

	if wantComponentAsset {
		result = append(result, source.Data{
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
			Time: eventTime,
		})
	}

	return result, nil
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
