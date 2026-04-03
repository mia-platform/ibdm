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

// personalAccessTokenRequestProcessor handles "personal_access_token_request" webhook events.
type personalAccessTokenRequestProcessor struct {
	client *client
}

// patActionToOperation maps personal_access_token_request webhook actions to data operations.
var patActionToOperation = map[string]source.DataOperation{
	"approved":  source.DataOperationUpsert,
	"created":   source.DataOperationUpsert,
	"cancelled": source.DataOperationDelete,
	"denied":    source.DataOperationDelete,
}

func (p *personalAccessTokenRequestProcessor) process(_ context.Context, typesToStream map[string]source.Extra, body []byte) ([]source.Data, error) {
	if _, ok := typesToStream[personalAccessTokenRequestType]; !ok {
		return nil, nil
	}

	action, patRequestObject, err := parsePersonalAccessTokenRequestEvent(body)
	if err != nil {
		return nil, err
	}

	operation, ok := patActionToOperation[action]
	if !ok {
		return nil, nil
	}

	return []source.Data{
		{
			Type:      personalAccessTokenRequestType,
			Operation: operation,
			Values:    map[string]any{personalAccessTokenRequestType: patRequestObject},
			Time:      timeSource(),
		},
	}, nil
}

// parsePersonalAccessTokenRequestEvent extracts the action and personal_access_token_request
// object from a personal_access_token_request webhook payload.
func parsePersonalAccessTokenRequestEvent(body []byte) (string, map[string]any, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", nil, fmt.Errorf("failed to unmarshal personal_access_token_request event: %w", err)
	}

	action, ok := payload["action"].(string)
	if !ok || action == "" {
		return "", nil, errors.New("missing or invalid action field in personal_access_token_request event")
	}

	patRequestObject, ok := payload["personal_access_token_request"].(map[string]any)
	if !ok {
		return "", nil, errors.New("missing or invalid personal_access_token_request field in personal_access_token_request event")
	}

	return action, patRequestObject, nil
}
