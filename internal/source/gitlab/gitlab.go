// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package gitlab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/mia-platform/ibdm/internal/source"
)

const (
	loggerName = "ibdm:source:gitlab"

	projectResource     = "project"
	pipelineResource    = "pipeline"
	accessTokenResource = "accesstoken"
)

var (
	// ErrSourceCreation is returned when the source cannot be initialised.
	ErrSourceCreation = errors.New("source creation error")
	// ErrWebhookTokenMissing is returned when no webhook token is configured.
	ErrWebhookTokenMissing = errors.New("webhook token not configured")
	// ErrSignatureMismatch is returned when the incoming webhook token does not match.
	ErrSignatureMismatch = errors.New("webhook token mismatch")
	// ErrUnmarshalingEvent is returned when the webhook body cannot be decoded.
	ErrUnmarshalingEvent = errors.New("error unmarshaling event")
	// ErrRetrievingAssets is returned when an API listing call fails.
	ErrRetrievingAssets = errors.New("error retrieving assets")
)

// NewSource constructs a [Source] by reading its configuration from environment
// variables. It returns [ErrSourceCreation] if either the API config or the
// webhook config cannot be loaded.
func NewSource() (*Source, error) {
	srcCfg, err := loadSourceConfigFromEnv()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSourceCreation, err)
	}

	whCfg, err := loadWebhookConfigFromEnv()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSourceCreation, err)
	}

	return &Source{
		c: &gitLabClient{
			config: srcCfg,
			http:   newHTTPClient(),
		},
		webhookConfig: whCfg,
	}, nil
}

// StartSyncProcess performs a full synchronisation of the requested resource types
// by listing assets from the GitLab API and sending them to results. Supported
// types are "project" and "pipeline". Concurrent calls are a no-op.
func (s *Source) StartSyncProcess(ctx context.Context, typesToSync map[string]source.Extra, results chan<- source.Data) error {
	log := logger.FromContext(ctx).WithName(loggerName)

	if !s.syncLock.TryLock() {
		log.Debug("sync process already running")
		return nil
	}
	defer s.syncLock.Unlock()

	if _, ok := typesToSync[projectResource]; ok {
		if err := s.syncProjects(ctx, results); err != nil {
			return err
		}
	}

	if _, ok := typesToSync[pipelineResource]; ok {
		if err := s.syncPipelines(ctx, results); err != nil {
			return err
		}
	}

	if _, ok := typesToSync[accessTokenResource]; ok {
		if err := s.syncGroupAccessTokens(ctx, results); err != nil {
			return err
		}
	}

	return nil
}

// syncProjects iterates all GitLab projects page by page and sends upsert events to results.
func (s *Source) syncProjects(ctx context.Context, results chan<- source.Data) error {
	it := s.c.newProjectsIterator()
	for {
		projects, err := it.next(ctx)
		if errors.Is(err, ErrIteratorDone) {
			break
		}
		if err != nil {
			return fmt.Errorf("%w: %w", ErrRetrievingAssets, err)
		}

		for _, project := range projects {
			id, err := getIDFromItem(project)
			if err != nil {
				continue
			}

			langs, err := s.c.getProjectLanguages(ctx, id)
			if err != nil {
				return fmt.Errorf("%w: %w", ErrRetrievingAssets, err)
			}

			tokens, err := s.c.getProjectAccessTokens(ctx, id)
			if err != nil && !errors.Is(err, ErrNotAccessible) {
				return fmt.Errorf("%w: %w", ErrRetrievingAssets, err)
			}

			results <- source.Data{
				Type:      projectResource,
				Operation: source.DataOperationUpsert,
				Values:    projectWrapper(project, langs, tokens),
				Time:      updatedAtOrNow(project),
			}
		}
	}

	return nil
}

// syncPipelines iterates all projects and, for each project, iterates all pipelines
// page by page, sending upsert events to results.
func (s *Source) syncPipelines(ctx context.Context, results chan<- source.Data) error {
	projectIt := s.c.newProjectsIterator()
	for {
		projects, err := projectIt.next(ctx)
		if errors.Is(err, ErrIteratorDone) {
			break
		}
		if err != nil {
			return fmt.Errorf("%w: %w", ErrRetrievingAssets, err)
		}

		for _, project := range projects {
			projectID, err := getIDFromItem(project)
			if err != nil {
				continue
			}

			pipelineIt := s.c.newProjectResourcesIterator(pipelineResource, projectID)
			for {
				pipelines, err := pipelineIt.next(ctx)
				if errors.Is(err, ErrIteratorDone) {
					break
				}
				if err != nil {
					return fmt.Errorf("%w: %w", ErrRetrievingAssets, err)
				}

				for _, pipeline := range pipelines {
					results <- source.Data{
						Type:      pipelineResource,
						Operation: source.DataOperationUpsert,
						Values:    pipeline,
						Time:      updatedAtOrNow(pipeline),
					}
				}
			}
		}
	}

	return nil
}

// syncGroupAccessTokens iterates all GitLab groups and, for each group, iterates all access tokens
// page by page, sending upsert events to results.
func (s *Source) syncGroupAccessTokens(ctx context.Context, results chan<- source.Data) error {
	groupIt := s.c.newGroupsIterator()
	for {
		groups, err := groupIt.next(ctx)
		if errors.Is(err, ErrIteratorDone) {
			break
		}
		if err != nil {
			return fmt.Errorf("%w: %w", ErrRetrievingAssets, err)
		}

		for _, group := range groups {
			groupID, err := getIDFromItem(group)
			if err != nil {
				continue
			}

			tokenIt := s.c.newGroupResourcesIterator(accessTokenResource, groupID)
			for {
				tokens, err := tokenIt.next(ctx)
				if errors.Is(err, ErrIteratorDone) {
					break
				}
				if err != nil {
					return fmt.Errorf("%w: %w", ErrRetrievingAssets, err)
				}

				for _, token := range tokens {
					results <- source.Data{
						Type:      accessTokenResource,
						Operation: source.DataOperationUpsert,
						Values:    token,
						Time:      updatedAtOrNow(token),
					}
				}
			}
		}
	}

	return nil
}

// GetWebhook returns a [source.Webhook] that validates incoming GitLab pipeline
// webhook requests using a plain-text token comparison and dispatches matching
// events to results asynchronously. It returns [ErrWebhookTokenMissing] when no
// token is configured.
func (s *Source) GetWebhook(ctx context.Context, typesToStream map[string]source.Extra, results chan<- source.Data) (source.Webhook, error) {
	if s.webhookConfig.WebhookToken == "" {
		return source.Webhook{}, ErrWebhookTokenMissing
	}

	log := logger.FromContext(ctx).WithName(loggerName)

	return source.Webhook{
		Method: http.MethodPost,
		Path:   s.webhookConfig.WebhookPath,
		Handler: func(ctx context.Context, headers http.Header, body []byte) error {
			if headers.Get(gitlabTokenHeader) != s.webhookConfig.WebhookToken {
				log.Error("webhook token validation failed")
				return ErrSignatureMismatch
			}

			if headers.Get(gitlabEventHeader) != pipelineHookHeaderValue {
				log.Debug("ignoring non-pipeline event", gitlabEventHeader, headers.Get(gitlabEventHeader))
				return nil
			}

			ev, err := s.parsePipelineEvent(ctx, body)
			if err != nil {
				log.Error(ErrUnmarshalingEvent.Error(), "error", err.Error())
				return fmt.Errorf("%w: %w", ErrUnmarshalingEvent, err)
			}

			if ev.ObjectKind != pipelineEventKind {
				log.Debug("ignoring event with unexpected object_kind", "object_kind", ev.ObjectKind)
				return nil
			}

			if _, ok := typesToStream[pipelineResource]; !ok {
				log.Debug("ignoring pipeline event: pipeline type not requested")
				return nil
			}

			go func() {
				results <- source.Data{
					Type:      pipelineResource,
					Operation: source.DataOperationUpsert,
					Values:    ev.ToValues(),
					Time:      ev.EventTime(),
				}
			}()

			return nil
		},
	}, nil
}

// parsePipelineEvent decodes a raw webhook body into a pipelineEvent, preserving
// the full payload for use as source.Data.Values.
func (s *Source) parsePipelineEvent(ctx context.Context, body []byte) (*pipelineEvent, error) {
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

	tokens, err := s.c.getProjectAccessTokens(ctx, strconv.Itoa(int(projectID)))
	if err != nil && !errors.Is(err, ErrNotAccessible) {
		return nil, err
	}

	return &pipelineEvent{
		ObjectKind:       objectKind,
		ObjectAttributes: objectAttributes,
		rawValues:        pipeline,
		project:          projectWrapper(project, langs, tokens),
	}, nil
}

// updatedAtOrNow reads the updated_at field from a GitLab API item and parses it
// as RFC3339. When absent or unparsable it falls back to time.Now().
func updatedAtOrNow(item map[string]any) time.Time {
	if updatedAt, ok := item["updated_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, updatedAt); err == nil {
			return t
		}
	}

	return time.Now()
}

// getIDFromItem extracts the numeric ID from a GitLab API item.
func getIDFromItem(item map[string]any) (string, error) {
	idFloat, ok := item["id"].(float64)
	if !ok {
		return "", errors.New("item missing id field")
	}

	return strconv.FormatInt(int64(idFloat), 10), nil
}

// projectWrapper wraps a project, its languages, and its access tokens into a single map.
func projectWrapper(project, languages map[string]any, tokens []map[string]any) map[string]any {
	projectWrapped := make(map[string]any)
	projectWrapped["project"] = project
	projectWrapped["project_languages"] = languages
	projectWrapped["project_access_tokens"] = tokens
	return projectWrapped
}
