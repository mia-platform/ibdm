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

func (p *pipelineEventProcessor) process(ctx context.Context, s *Source, typesToStream map[string]source.Extra, body []byte) ([]source.Data, error) {
	ev, err := parsePipelineEvent(ctx, s, body)
	if err != nil {
		return nil, err
	}

	if ev.ObjectKind != pipelineEventKind {
		return nil, nil
	}

	if _, ok := typesToStream[pipelineResource]; !ok {
		return nil, nil
	}

	return []source.Data{
		{
			Type:      pipelineResource,
			Operation: source.DataOperationUpsert,
			Values:    ev.ToValues(),
			Time:      ev.EventTime(),
		},
	}, nil
}

// parsePipelineEvent decodes a raw webhook body into a pipelineEvent, preserving
// the full payload for use as source.Data.Values.
func parsePipelineEvent(ctx context.Context, s *Source, body []byte) (*pipelineEvent, error) {
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

	project, err := s.c.getProject(ctx, int(projectID))
	if err != nil {
		return nil, err
	}

	langs, err := s.c.getProjectLanguages(ctx, strconv.Itoa(int(projectID)))
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
