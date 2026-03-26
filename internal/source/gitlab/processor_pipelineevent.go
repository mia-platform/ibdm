// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package gitlab

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/mia-platform/ibdm/internal/source"
)

const (
	// pipelineEventKind is the object_kind value for pipeline webhook events.
	pipelineEventKind = "pipeline"

	// pipelineHookHeaderValue is the expected value of X-Gitlab-Event for pipeline events.
	pipelineHookHeaderValue = "Pipeline Hook"
)

// pipelineEvent represents a GitLab pipeline webhook payload.
type pipelineEvent struct {
	ObjectKind       string         `json:"object_kind"`       //nolint:tagliatelle // GitLab API uses snake_case
	ObjectAttributes map[string]any `json:"object_attributes"` //nolint:tagliatelle // GitLab API uses snake_case

	pipeline map[string]any
	project  map[string]any
}

var _ gitlabEvent = &pipelineEvent{}

// EventTime returns the time of the pipeline event. It reads the updated_at field
// from object_attributes when present, and falls back to time.Now().
func (e *pipelineEvent) EventTime() time.Time {
	if e.ObjectAttributes == nil {
		return time.Now()
	}

	if updatedAt, ok := e.ObjectAttributes["updated_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, updatedAt); err == nil {
			return t
		}
	}

	return time.Now()
}

// pipelineEventProcessor handles "Pipeline Hook" webhook events.
type pipelineEventProcessor struct{}

func (p *pipelineEventProcessor) process(ctx context.Context, c *gitLabClient, typesToStream map[string]source.Extra, body []byte) ([]source.Data, error) {
	var eventsToMap []source.Data

	// Decode only the object_kind before doing any API calls.
	var raw struct {
		ObjectKind string `json:"object_kind"` //nolint:tagliatelle // GitLab API uses snake_case
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	if raw.ObjectKind != pipelineEventKind {
		return nil, nil
	}

	ev, err := parsePipelineEvent(ctx, c, body)
	if err != nil {
		return nil, err
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
			"pipeline": ev.pipeline,
			"project":  ev.project["project"],
		},
		Time: ev.EventTime(),
	})

	return eventsToMap, nil
}

// parsePipelineEvent decodes a raw webhook body into a pipelineEvent, preserving
// the full payload for use as source.Data.Values.
func parsePipelineEvent(ctx context.Context, c *gitLabClient, body []byte) (*pipelineEvent, error) {
	var pipelineEv map[string]any
	if err := json.Unmarshal(body, &pipelineEv); err != nil {
		return nil, err
	}

	objectKind, _ := pipelineEv["object_kind"].(string)
	objectAttributes, _ := pipelineEv["object_attributes"].(map[string]any)
	project, _ := pipelineEv["project"].(map[string]any)
	if project == nil {
		return nil, errors.New("event payload missing project field")
	}

	projectID, ok := project["id"].(float64)
	if !ok {
		return nil, errors.New("event payload project.id is missing or invalid")
	}

	if objectAttributes == nil {
		return nil, errors.New("event payload missing object_attributes field")
	}
	pipelineID, ok := objectAttributes["id"].(float64)
	if !ok {
		return nil, errors.New("event payload object_attributes.id is missing or invalid")
	}

	projectIDStr := strconv.Itoa(int(projectID))

	project, err := c.getProject(ctx, int(projectID))
	if err != nil {
		return nil, err
	}

	langs, err := c.getProjectLanguages(ctx, projectIDStr)
	if err != nil {
		return nil, err
	}

	pipeline, err := c.getPipeline(ctx, projectIDStr, strconv.Itoa(int(pipelineID)))
	if err != nil {
		return nil, err
	}

	return &pipelineEvent{
		ObjectKind:       objectKind,
		ObjectAttributes: objectAttributes,
		pipeline:         pipeline,
		project:          projectWrapper(project, langs),
	}, nil
}
