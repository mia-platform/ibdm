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
	// pushEventKind is the object_kind value for push webhook events.
	pushEventKind = "push"

	// pushHookHeaderValue is the expected value of X-Gitlab-Event for push events.
	pushHookHeaderValue = "Push Hook"
)

// pushEvent represents a GitLab push webhook payload.
type pushEvent struct {
	ObjectKind string `json:"object_kind"` //nolint:tagliatelle // GitLab API uses snake_case
	Commits    []map[string]any

	project map[string]any
}

var _ gitlabEvent = &pushEvent{}

// EventTime returns time.Now() since push events have no adequate timestamp field.
func (e *pushEvent) EventTime() time.Time {
	return time.Now()
}

// pushEventProcessor handles "Push Hook" webhook events.
type pushEventProcessor struct{}

func (p *pushEventProcessor) process(ctx context.Context, c *gitLabClient, typesToStream map[string]source.Extra, body []byte) ([]source.Data, error) {
	// Decode only the object_kind before doing any API calls.
	var raw struct {
		ObjectKind string `json:"object_kind"` //nolint:tagliatelle // GitLab API uses snake_case
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	if raw.ObjectKind != pushEventKind {
		return nil, nil
	}

	ev, err := parsePushEvent(ctx, c, body)
	if err != nil {
		return nil, err
	}

	if _, ok := typesToStream[projectResource]; !ok {
		return nil, nil
	}

	return []source.Data{
		{
			Type:      projectResource,
			Operation: source.DataOperationUpsert,
			Values:    ev.project,
			Time:      ev.EventTime(),
		},
	}, nil
}

// parsePushEvent decodes a raw webhook body into a pushEvent, fetching the full
// project object and its languages from the GitLab API.
func parsePushEvent(ctx context.Context, c *gitLabClient, body []byte) (*pushEvent, error) {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}

	objectKind, _ := raw["object_kind"].(string)

	projectID, ok := raw["project_id"].(float64)
	if !ok {
		return nil, errors.New("event payload missing project_id field")
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

	return &pushEvent{
		ObjectKind: objectKind,
		project:    projectWrapper(project, langs),
	}, nil
}
