// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package bitbucket

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/mia-platform/ibdm/internal/source"
)

const (
	loggerName = "ibdm:source:bitbucket"

	// repositoryType is the data type key for Bitbucket repositories.
	repositoryType = "repository"

	// pipelineType is the data type key for Bitbucket pipelines.
	pipelineType = "pipeline"

	// fullNamePartCount is the number of parts in a Bitbucket full_name ("workspace/repo").
	fullNamePartCount = 2
)

var (
	// ErrRetrievingAssets wraps errors that occur while fetching API resources during sync.
	ErrRetrievingAssets = errors.New("error retrieving assets")
)

// timeSource is a package-level function for the current time, replaceable in tests.
var timeSource = time.Now

// StartSyncProcess performs a full synchronisation of the requested resource
// types by querying the Bitbucket REST API and sending results to results.
// Only known data types are processed; unknown types are skipped with a debug
// log message.
func (s *Source) StartSyncProcess(ctx context.Context, typesToSync map[string]source.Extra, results chan<- source.Data) error {
	log := logger.FromContext(ctx).WithName(loggerName)
	if !s.syncLock.TryLock() {
		log.Debug("sync process already running")
		return nil
	}
	defer s.syncLock.Unlock()

	_, wantsRepo := typesToSync[repositoryType]
	_, wantsPipeline := typesToSync[pipelineType]

	if wantsRepo || wantsPipeline {
		if err := s.syncWorkspaceAssets(ctx, typesToSync, results); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil
			}
			log.Error("error syncing workspace assets", "error", err.Error())
			return fmt.Errorf("%w: %w", ErrBitbucketSource, err)
		}
	}

	knownTypes := map[string]bool{
		repositoryType: true,
		pipelineType:   true,
	}
	for dataType := range typesToSync {
		if !knownTypes[dataType] {
			log.Debug("skipping unknown data type", "type", dataType)
		}
	}

	return nil
}

// syncWorkspaceAssets orchestrates workspace-level sync. When BITBUCKET_WORKSPACE
// is set, it delegates directly; otherwise it iterates all accessible workspaces.
func (s *Source) syncWorkspaceAssets(ctx context.Context, typesToSync map[string]source.Extra, results chan<- source.Data) error {
	log := logger.FromContext(ctx).WithName(loggerName)
	if s.workspace != "" {
		return s.syncRepositoriesForWorkspace(ctx, s.workspace, typesToSync, results)
	}

	workspaceIt := s.client.listWorkspaces()
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}

		workspaceAccessItems, err := workspaceIt.next(ctx)
		if errors.Is(err, ErrIteratorDone) {
			break
		}
		if err != nil {
			return fmt.Errorf("%w: %w", ErrRetrievingAssets, err)
		}

		for _, workspaceAccess := range workspaceAccessItems {
			if err := ctx.Err(); err != nil {
				return nil
			}
			slug := extractWorkspaceSlug(workspaceAccess)
			if slug == "" {
				log.Debug("skipping workspace with empty slug")
				continue
			}
			if err := s.syncRepositoriesForWorkspace(ctx, slug, typesToSync, results); err != nil {
				return err
			}
		}
	}

	return nil
}

// syncRepositoriesForWorkspace iterates repositories for a single workspace and,
// depending on the requested types, emits repository entries and/or fetches
// pipelines per repository.
func (s *Source) syncRepositoriesForWorkspace(ctx context.Context, slug string, typesToSync map[string]source.Extra, results chan<- source.Data) error {
	log := logger.FromContext(ctx).WithName(loggerName)
	_, syncRepo := typesToSync[repositoryType]
	_, syncPipeline := typesToSync[pipelineType]

	repoIt := s.client.listRepositories(slug)
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}

		repos, err := repoIt.next(ctx)
		if errors.Is(err, ErrIteratorDone) {
			break
		}
		if err != nil {
			return fmt.Errorf("%w: %w", ErrRetrievingAssets, err)
		}

		for _, repo := range repos {
			if err := ctx.Err(); err != nil {
				return nil
			}

			if syncRepo {
				results <- source.Data{
					Type:      repositoryType,
					Operation: source.DataOperationUpsert,
					Values: map[string]any{
						"repository": repo,
					},
					Time: timeSource(),
				}
			}

			if syncPipeline {
				if err := s.syncRepositoryPipelines(ctx, slug, repo, results); err != nil {
					log.Error("error syncing pipelines for repository, skipping", "workspace", slug, "repo", extractRepoSlug(repo), "error", err.Error())
				}
			}
		}
	}
	return nil
}

// syncRepositoryPipelines fetches all pipelines for a single repository and
// pushes each as a source.Data entry onto the results channel.
func (s *Source) syncRepositoryPipelines(ctx context.Context, workspaceSlug string, repo map[string]any, results chan<- source.Data) error {
	repoSlug := extractRepoSlug(repo)
	if repoSlug == "" {
		return nil
	}

	pipelineIt := s.client.listPipelines(workspaceSlug, repoSlug)
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}

		pipelines, err := pipelineIt.next(ctx)
		if errors.Is(err, ErrIteratorDone) {
			break
		}
		if err != nil {
			return fmt.Errorf("%w: %w", ErrRetrievingAssets, err)
		}

		for _, pipeline := range pipelines {
			results <- source.Data{
				Type:      pipelineType,
				Operation: source.DataOperationUpsert,
				Values: map[string]any{
					"repository": repo,
					"pipeline":   pipeline,
				},
				Time: timeSource(),
			}
		}
	}
	return nil
}

// extractWorkspaceSlug extracts the workspace slug from a workspace_access item returned
// by GET /2.0/user/workspaces. It drills into the nested "workspace" object. Falls back
// to a top-level "slug" field in case the input is already a plain workspace object.
func extractWorkspaceSlug(workspaceAccess map[string]any) string {
	if ws, ok := workspaceAccess["workspace"].(map[string]any); ok {
		if slug, ok := ws["slug"].(string); ok {
			return slug
		}
	}
	if slug, ok := workspaceAccess["slug"].(string); ok {
		return slug
	}
	return ""
}

// extractRepoSlug extracts the "slug" field from a repository object.
// Falls back to "name" if "slug" is absent.
func extractRepoSlug(repo map[string]any) string {
	if slug, ok := repo["slug"].(string); ok {
		return slug
	}
	if name, ok := repo["name"].(string); ok {
		return name
	}
	return ""
}

// pipelineTimeOrNow reads the "completed_on" field from a pipeline object and parses it
// as RFC3339. When absent or unparsable, it tries "created_on", and finally
// falls back to timeSource().
func pipelineTimeOrNow(item map[string]any) time.Time {
	if completedOn, ok := item["completed_on"].(string); ok {
		if t, err := time.Parse(time.RFC3339, completedOn); err == nil {
			return t
		}
	}
	if createdOn, ok := item["created_on"].(string); ok {
		if t, err := time.Parse(time.RFC3339, createdOn); err == nil {
			return t
		}
	}
	return timeSource()
}

// splitFullName splits a Bitbucket full_name ("workspace/repo") into its parts.
func splitFullName(fullName string) (workspace, repo string) {
	parts := strings.SplitN(fullName, "/", fullNamePartCount)
	if len(parts) != fullNamePartCount {
		return "", ""
	}
	return parts[0], parts[1]
}
