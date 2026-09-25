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

	// apiVersionKey is the mapping extra key holding the API version to use for a data type.
	apiVersionKey = "apiVersion"
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
func (s *Source) StartSyncProcess(ctx context.Context, typesToSync map[string]source.MappingExtras, results chan<- source.Data) error {
	log := logger.FromContext(ctx).WithName(loggerName)
	if !s.syncLock.TryLock() {
		log.Debug("sync process already running")
		return nil
	}
	defer s.syncLock.Unlock()

	warnUnusableAPIVersions(log, typesToSync, repositoryType, workflowRunType)

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

// repositoryPass describes one listing of the organization repositories made by a sync.
type repositoryPass struct {
	// apiVersion is the API version the repositories are listed, and their languages fetched, with.
	apiVersion string
	// emitRepositories reports whether the pass emits the repositories it lists.
	emitRepositories bool
	// repositoryTargets is the Data.Mappings value of the repositories the pass emits.
	repositoryTargets []string
	// runs lists the workflow runs fetches the pass makes for every repository it lists.
	runs []runsFetch
}

// runsFetch describes the workflow runs fetch of one API version.
type runsFetch struct {
	// apiVersion is the API version the workflow runs are fetched with.
	apiVersion string
	// targets is the Data.Mappings value of the workflow runs fetched.
	targets []string
}

// repositoryPasses plans the repository listings a sync makes for typesToSync.
//
// Repositories are listed once per API version their mappings declare, because the listing
// payload depends on the version and every mapping must receive the payload of its own version.
// Workflow runs only need the owner and the name of a repository, so they are fetched during the
// first listing alone, once per API version their mappings declare: repeating them on every
// listing would emit every run once per repository API version. When only workflow runs are
// requested, a single listing is made with the first of their API versions.
func repositoryPasses(typesToSync map[string]source.MappingExtras) []repositoryPass {
	repoExtras, syncRepo := typesToSync[repositoryType]
	runExtras, syncRuns := typesToSync[workflowRunType]

	var runs []runsFetch
	if syncRuns {
		for _, group := range apiVersionGroups(runExtras) {
			runs = append(runs, runsFetch{apiVersion: group.Key, targets: runExtras.Target(group.Mappings)})
		}
	}

	if !syncRepo {
		if len(runs) == 0 {
			return nil
		}
		return []repositoryPass{{apiVersion: runs[0].apiVersion, runs: runs}}
	}

	repoGroups := apiVersionGroups(repoExtras)
	passes := make([]repositoryPass, 0, len(repoGroups))
	for i, group := range repoGroups {
		pass := repositoryPass{
			apiVersion:        group.Key,
			emitRepositories:  true,
			repositoryTargets: repoExtras.Target(group.Mappings),
		}
		if i == 0 {
			pass.runs = runs
		}
		passes = append(passes, pass)
	}

	return passes
}

// syncRepositoryAssets lists the repositories of the configured organization once per planned
// pass and, for each repository, emits a repository entry and/or fetches workflow runs
// depending on which types are present in typesToSync. See repositoryPasses.
func (s *Source) syncRepositoryAssets(ctx context.Context, typesToSync map[string]source.MappingExtras, results chan<- source.Data) error {
	for _, pass := range repositoryPasses(typesToSync) {
		if err := s.syncRepositoryPass(ctx, pass, results); err != nil {
			return err
		}
	}

	return nil
}

// syncRepositoryPass makes the repository listing pass describes.
func (s *Source) syncRepositoryPass(ctx context.Context, pass repositoryPass, results chan<- source.Data) error {
	it := s.client.listRepositories(pass.apiVersion)
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

			if pass.emitRepositories {
				fullName, _ := item["full_name"].(string)
				values := map[string]any{repositoryType: item}
				if fullName != "" {
					if langs, err := s.client.getRepositoryLanguages(ctx, fullName, pass.apiVersion); err == nil {
						values["repositoryLanguages"] = langs
					}
				}
				results <- source.Data{
					Type:      repositoryType,
					Operation: source.DataOperationUpsert,
					Values:    values,
					Time:      timeSource(),
					Mappings:  pass.repositoryTargets,
				}
			}

			for _, runs := range pass.runs {
				if err := s.syncRepositoryWorkflowRuns(ctx, item, runs.apiVersion, runs.targets, results); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// apiVersionGroups groups the mappings of a data type by the API version they fetch with, as
// apiVersionFromExtra resolves it, so every mapping belongs to a group. A data type requested
// without any mapping still fetches once, with defaultAPIVersion.
func apiVersionGroups(extras source.MappingExtras) []source.MappingGroup {
	groups := extras.GroupBy(func(extra source.Extra) (string, bool) {
		return apiVersionFromExtra(extra), true
	})
	if len(groups) == 0 {
		return []source.MappingGroup{{Key: defaultAPIVersion}}
	}

	return groups
}

// repositoryEmissions builds the data a repository webhook event emits for repoObject. When the
// languages of the repository can be fetched, they are fetched once per API version the mappings
// in extras declare, and each emission targets the mappings of its version. Otherwise nothing is
// fetched, every mapping receives the same payload, and a single untargeted emission is built.
func repositoryEmissions(ctx context.Context, c *client, extras source.MappingExtras, repoObject map[string]any, operation source.DataOperation) []source.Data {
	timestamp := timeSource()
	fullName, _ := repoObject["full_name"].(string)
	if c == nil || fullName == "" {
		return []source.Data{{
			Type:      repositoryType,
			Operation: operation,
			Values:    map[string]any{repositoryType: repoObject},
			Time:      timestamp,
		}}
	}

	groups := apiVersionGroups(extras)
	data := make([]source.Data, 0, len(groups))
	for _, group := range groups {
		values := map[string]any{repositoryType: repoObject}
		if langs, err := c.getRepositoryLanguages(ctx, fullName, group.Key); err == nil {
			values["repositoryLanguages"] = langs
		}
		data = append(data, source.Data{
			Type:      repositoryType,
			Operation: operation,
			Values:    values,
			Time:      timestamp,
			Mappings:  extras.Target(group.Mappings),
		})
	}

	return data
}

// warnUnusableAPIVersions logs a warning for every mapping of dataTypes whose apiVersion is
// declared but is not a non-empty string, typically an unquoted date that YAML decodes as a
// timestamp: apiVersionFromExtra silently falls back to defaultAPIVersion for such a mapping.
func warnUnusableAPIVersions(log logger.Logger, typesToStream map[string]source.MappingExtras, dataTypes ...string) {
	for _, dataType := range dataTypes {
		extras := typesToStream[dataType]
		for _, mapping := range extras.InvalidStringValues(apiVersionKey) {
			log.Warn("mapping apiVersion is not a non-empty string, the default API version is used instead: quote it in the mapping file",
				"type", dataType, "mapping", mapping, "valueType", fmt.Sprintf("%T", extras[mapping][apiVersionKey]), "defaultAPIVersion", defaultAPIVersion)
		}
	}
}

// apiVersionFromExtra extracts the API version from the mapping extra config.
// Falls back to defaultAPIVersion if absent or empty.
func apiVersionFromExtra(extra source.Extra) string {
	if v, ok := extra[apiVersionKey]; ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return defaultAPIVersion
}

// syncRepositoryWorkflowRuns fetches all workflow runs for the given repository
// and pushes each as a source.Data entry onto the results channel.
// targets is the Data.Mappings value of the emitted runs.
func (s *Source) syncRepositoryWorkflowRuns(ctx context.Context, repo map[string]any, apiVersion string, targets []string, results chan<- source.Data) error {
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
				Mappings:  targets,
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
