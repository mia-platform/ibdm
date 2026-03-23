// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package gitlab

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/mia-platform/ibdm/internal/source"
)

// pipelineEventProcessor handles "Pipeline Hook" webhook events.
type pipelineEventProcessor struct{}

func (p *pipelineEventProcessor) process(ctx context.Context, c *gitLabClient, typesToStream map[string]source.Extra, body []byte) ([]source.Data, error) {
	var eventsToMap []source.Data
	ev, err := parsePipelineEvent(ctx, c, body)
	if err != nil {
		return nil, err
	}

	if ev.ObjectKind != pipelineEventKind {
		return nil, nil
	}

	if _, ok := typesToStream[projectResource]; !ok {
		return nil, nil
	}

	eventsToMap = append(eventsToMap, source.Data{
		Type:      projectResource,
		Operation: source.DataOperationUpsert,
		Values:    ev.project,
		Time:      ev.EventTime(),
	})

	if _, ok := typesToStream[pipelineResource]; !ok {
		return nil, nil
	}

	eventsToMap = append(eventsToMap, source.Data{
		Type:      pipelineResource,
		Operation: source.DataOperationUpsert,
		Values: map[string]any{
			"pipeline": ev.ObjectAttributes,
		},
		Time: ev.EventTime(),
	})

	return eventsToMap, nil
}

// parsePipelineEvent decodes a raw webhook body into a pipelineEvent, preserving
// the full payload for use as source.Data.Values.
func parsePipelineEvent(ctx context.Context, c *gitLabClient, body []byte) (*pipelineEvent, error) {
	var pipeline map[string]any
	if err := json.Unmarshal(body, &pipeline); err != nil {
		return nil, err
	}

	objectKind, _ := pipeline["object_kind"].(string)
	objectAttributes, _ := pipeline["object_attributes"].(map[string]any)
	project, _ := pipeline["project"].(map[string]any)
	if project == nil {
		return nil, errors.New("event payload missing project field")
	}

	projectID, ok := project["id"].(float64)
	if !ok {
		return nil, errors.New("event payload project.id is missing or invalid")
	}

	project, err := c.getProject(ctx, int(projectID))
	if err != nil {
		return nil, err
	}

	langs, err := c.getProjectLanguages(ctx, strconv.Itoa(int(projectID)))
	if err != nil {
		return nil, err
	}

	return &pipelineEvent{
		ObjectKind:       objectKind,
		ObjectAttributes: objectAttributes,
		rawValues:        pipeline,
		project:          projectWrapper(project, langs),
	}, nil
}
