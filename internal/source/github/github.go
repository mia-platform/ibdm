// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/mia-platform/ibdm/internal/source"
)

const (
	loggerName = "ibdm:source:github"

	// repositoryType is the data type key for GitHub repositories.
	repositoryType = "repository"

	// personalAccessTokenRequestType is the data type key for GitHub PAT requests.
	personalAccessTokenRequestType = "personal_access_token_request"

	// workflowDispatchType is the data type key for GitHub workflow dispatch events.
	workflowDispatchType = "workflow_dispatch"

	// workflowRunType is the data type key for GitHub workflow runs.
	workflowRunType = "workflow_run"

	// defaultAPIVersion is the GitHub REST API version used when the mapping
	// config does not explicitly set extra["apiVersion"].
	defaultAPIVersion = "2026-03-10"
)

var (
	// ErrGitHubSource wraps errors emitted by the GitHub source implementation.
	ErrGitHubSource = errors.New("github source")
	// ErrRetrievingAssets wraps errors that occur while fetching API resources.
	ErrRetrievingAssets = errors.New("error retrieving assets")

	// timeSource is a package-level function for the current time, replaceable in tests.
	timeSource = time.Now
)

var _ source.SyncableSource = &Source{}
var _ source.WebhookSource = &Source{}

// Source implements source.SyncableSource and source.WebhookSource for GitHub.
type Source struct {
	config config
	client *client

	syncLock sync.Mutex
}

// NewSource constructs a Source by reading its configuration from environment
// variables and initialising the underlying HTTP client. It returns
// ErrGitHubSource if the configuration is invalid.
func NewSource() (*Source, error) {
	cfg, err := loadConfigFromEnv()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrGitHubSource, err)
	}

	return &Source{
		config: *cfg,
		client: &client{
			baseURL:  cfg.URL,
			org:      cfg.Org,
			token:    cfg.Token,
			pageSize: cfg.PageSize,
			httpClient: &http.Client{
				Timeout: cfg.HTTPTimeout,
			},
		},
	}, nil
}

// StartSyncProcess performs a full synchronisation of the requested resource
// types by querying the GitHub REST API and sending results to results.
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
	_, wantsRuns := typesToSync[workflowRunType]
	if wantsRepo || wantsRuns {
		if err := s.syncRepositoryAssets(ctx, typesToSync, results); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil
			}
			log.Error("error syncing repository assets", "error", err.Error())
			return fmt.Errorf("%w: %w", ErrGitHubSource, err)
		}
	}

	knownTypes := map[string]bool{
		repositoryType:  true,
		workflowRunType: true,
	}
	for dataType := range typesToSync {
		if !knownTypes[dataType] {
			log.Debug("skipping unknown data type", "type", dataType)
		}
	}

	return nil
}

// syncRepositoryAssets iterates all repositories for the configured organization once
// and, for each repository, emits a repository entry and/or fetches workflow runs
// depending on which types are present in typesToSync.
func (s *Source) syncRepositoryAssets(ctx context.Context, typesToSync map[string]source.Extra, results chan<- source.Data) error {
	_, syncRepo := typesToSync[repositoryType]
	_, syncRuns := typesToSync[workflowRunType]

	var repoAPIVersion, runAPIVersion string
	if syncRepo {
		repoAPIVersion = apiVersionFromExtra(typesToSync[repositoryType])
	}
	if syncRuns {
		runAPIVersion = apiVersionFromExtra(typesToSync[workflowRunType])
	}

	// Use repo API version for the repository listing; fall back to run version
	// when only workflow runs are requested.
	listAPIVersion := repoAPIVersion
	if listAPIVersion == "" {
		listAPIVersion = runAPIVersion
	}

	it := s.client.listRepositories(listAPIVersion)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		items, err := it.next(ctx)
		if errors.Is(err, ErrIteratorDone) {
			break
		}
		if err != nil {
			return fmt.Errorf("%w: %w", ErrRetrievingAssets, err)
		}

		for _, item := range items {
			if err := ctx.Err(); err != nil {
				return err
			}

			if syncRepo {
				fullName, _ := item["full_name"].(string)
				values := map[string]any{repositoryType: item}
				if fullName != "" {
					if langs, err := s.client.getRepositoryLanguages(ctx, fullName, repoAPIVersion); err == nil {
						values["repositoryLanguages"] = langs
					}
				}
				results <- source.Data{
					Type:      repositoryType,
					Operation: source.DataOperationUpsert,
					Values:    values,
					Time:      timeSource(),
				}
			}

			if syncRuns {
				if err := s.syncRepositoryWorkflowRuns(ctx, item, runAPIVersion, results); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// apiVersionFromExtra extracts the API version from the mapping extra config.
// Falls back to defaultAPIVersion if absent or empty.
func apiVersionFromExtra(extra source.Extra) string {
	if v, ok := extra["apiVersion"]; ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return defaultAPIVersion
}

// syncRepositoryWorkflowRuns fetches all workflow runs for the given repository
// and pushes each as a source.Data entry onto the results channel.
func (s *Source) syncRepositoryWorkflowRuns(ctx context.Context, repo map[string]any, apiVersion string, results chan<- source.Data) error {
	owner, repoName := extractOwnerRepo(repo)
	if owner == "" || repoName == "" {
		return nil
	}

	runIt := s.client.listWorkflowRuns(owner, repoName, apiVersion)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		items, err := runIt.next(ctx)
		if errors.Is(err, ErrIteratorDone) {
			break
		}
		if err != nil {
			return fmt.Errorf("%w: %w", ErrRetrievingAssets, err)
		}

		for _, item := range items {
			results <- source.Data{
				Type:      workflowRunType,
				Operation: source.DataOperationUpsert,
				Values:    map[string]any{workflowRunType: item},
				Time:      timeSource(),
			}
		}
	}
	return nil
}

// extractOwnerRepo extracts the owner login and repository name from a
// repository object. Returns empty strings if the fields are missing.
func extractOwnerRepo(repo map[string]any) (string, string) {
	name, _ := repo["name"].(string)
	ownerObj, _ := repo["owner"].(map[string]any)
	if ownerObj == nil {
		return "", name
	}
	login, _ := ownerObj["login"].(string)
	return login, name
}
