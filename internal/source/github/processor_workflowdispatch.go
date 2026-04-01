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

// workflowDispatchProcessor handles "workflow_dispatch" webhook events.
type workflowDispatchProcessor struct{}

func (p *workflowDispatchProcessor) process(_ context.Context, typesToStream map[string]source.Extra, body []byte) ([]source.Data, error) {
	if _, ok := typesToStream[workflowDispatchType]; !ok {
		return nil, nil
	}

	payload, err := parseWorkflowDispatchEvent(body)
	if err != nil {
		return nil, err
	}

	return []source.Data{
		{
			Type:      workflowDispatchType,
			Operation: source.DataOperationUpsert,
			Values:    map[string]any{workflowDispatchType: payload},
			Time:      timeSource(),
		},
	}, nil
}

// parseWorkflowDispatchEvent validates and returns the entire workflow_dispatch
// webhook payload as a map.
func parseWorkflowDispatchEvent(body []byte) (map[string]any, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal workflow_dispatch event: %w", err)
	}

	if _, ok := payload["workflow"].(string); !ok {
		return nil, errors.New("missing or invalid workflow field in workflow_dispatch event")
	}

	if _, ok := payload["ref"].(string); !ok {
		return nil, errors.New("missing or invalid ref field in workflow_dispatch event")
	}

	if _, ok := payload["repository"]; !ok {
		return nil, errors.New("missing repository field in workflow_dispatch event")
	}

	return payload, nil
}
