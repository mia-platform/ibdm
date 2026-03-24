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

// repositoryEventProcessor handles "repository" webhook events.
type repositoryEventProcessor struct{}

// actionToOperation maps repository webhook actions to data operations.
var actionToOperation = map[string]source.DataOperation{
	"created":     source.DataOperationUpsert,
	"edited":      source.DataOperationUpsert,
	"renamed":     source.DataOperationUpsert,
	"archived":    source.DataOperationUpsert,
	"unarchived":  source.DataOperationUpsert,
	"transferred": source.DataOperationUpsert,
	"publicized":  source.DataOperationUpsert,
	"privatized":  source.DataOperationUpsert,
	"deleted":     source.DataOperationDelete,
}

func (p *repositoryEventProcessor) process(_ context.Context, typesToStream map[string]source.Extra, body []byte) ([]source.Data, error) {
	if _, ok := typesToStream[repositoryType]; !ok {
		return nil, nil
	}

	action, repoObject, err := parseRepositoryEvent(body)
	if err != nil {
		return nil, err
	}

	operation, ok := actionToOperation[action]
	if !ok {
		return nil, nil
	}

	return []source.Data{
		{
			Type:      repositoryType,
			Operation: operation,
			Values:    map[string]any{repositoryType: repoObject},
			Time:      timeSource(),
		},
	}, nil
}

// parseRepositoryEvent extracts the action and repository object from a
// repository webhook payload.
func parseRepositoryEvent(body []byte) (string, map[string]any, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", nil, fmt.Errorf("failed to unmarshal repository event: %w", err)
	}

	action, ok := payload["action"].(string)
	if !ok || action == "" {
		return "", nil, errors.New("missing or invalid action field in repository event")
	}

	repoObject, ok := payload["repository"].(map[string]any)
	if !ok {
		return "", nil, errors.New("missing or invalid repository field in repository event")
	}

	return action, repoObject, nil
}
