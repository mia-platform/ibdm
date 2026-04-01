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

// workflowRunProcessor handles "workflow_run" webhook events.
type workflowRunProcessor struct{}

// workflowRunActionToOperation maps workflow_run webhook actions to data operations.
var workflowRunActionToOperation = map[string]source.DataOperation{
	"requested":   source.DataOperationUpsert,
	"in_progress": source.DataOperationUpsert,
	"completed":   source.DataOperationUpsert,
}

func (p *workflowRunProcessor) process(_ context.Context, typesToStream map[string]source.Extra, body []byte) ([]source.Data, error) {
	if _, ok := typesToStream[workflowRunType]; !ok {
		return nil, nil
	}

	action, workflowRunObject, err := parseWorkflowRunEvent(body)
	if err != nil {
		return nil, err
	}

	operation, ok := workflowRunActionToOperation[action]
	if !ok {
		return nil, nil
	}

	return []source.Data{
		{
			Type:      workflowRunType,
			Operation: operation,
			Values:    map[string]any{workflowRunType: workflowRunObject},
			Time:      timeSource(),
		},
	}, nil
}

// parseWorkflowRunEvent extracts the action and workflow_run object from a
// workflow_run webhook payload.
func parseWorkflowRunEvent(body []byte) (string, map[string]any, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", nil, fmt.Errorf("failed to unmarshal workflow_run event: %w", err)
	}

	action, ok := payload["action"].(string)
	if !ok || action == "" {
		return "", nil, errors.New("missing or invalid action field in workflow_run event")
	}

	workflowRunObject, ok := payload["workflow_run"].(map[string]any)
	if !ok {
		return "", nil, errors.New("missing or invalid workflow_run field in workflow_run event")
	}

	return action, workflowRunObject, nil
}
