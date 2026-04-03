// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/mia-platform/ibdm/internal/source"
)

// pushEventProcessor handles "push" webhook events.
type pushEventProcessor struct {
	client *client
}

func (p *pushEventProcessor) process(ctx context.Context, typesToStream map[string]source.Extra, body []byte) ([]source.Data, error) {
	if _, ok := typesToStream[repositoryType]; !ok {
		return nil, nil
	}

	repoObject, err := parsePushEvent(body)
	if err != nil {
		return nil, err
	}

	values := map[string]any{repositoryType: repoObject}
	if p.client != nil {
		if fullName, _ := repoObject["full_name"].(string); fullName != "" {
			apiVersion := apiVersionFromExtra(typesToStream[repositoryType])
			if langs, err := p.client.getRepositoryLanguages(ctx, fullName, apiVersion); err == nil {
				values["repositoryLanguages"] = langs
			}
		}
	}

	return []source.Data{
		{
			Type:      repositoryType,
			Operation: source.DataOperationUpsert,
			Values:    values,
			Time:      timeSource(),
		},
	}, nil
}

// parsePushEvent extracts the repository object from a push webhook payload.
func parsePushEvent(body []byte) (map[string]any, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal push event: %w", err)
	}

	repoObject, ok := payload["repository"].(map[string]any)
	if !ok {
		return nil, errors.New("missing or invalid repository field in push event")
	}

	return repoObject, nil
}
