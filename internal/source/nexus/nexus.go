// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package nexus

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/mia-platform/ibdm/internal/source"
)

const (
	loggerName = "ibdm:source:nexus"

	componentAssetType = "componentasset"
)

var (
	// ErrNexusSource wraps errors emitted by the Nexus source implementation.
	ErrNexusSource = errors.New("nexus source")

	// timeSource is a replaceable function for obtaining the current time.
	// Tests override this to produce deterministic timestamps.
	timeSource = time.Now
)

var _ source.SyncableSource = &Source{}

// Source implements source.SyncableSource for Nexus Repository Manager.
type Source struct {
	config config
	client *client

	syncLock sync.Mutex
}

// NewSource creates a new Nexus Source reading configuration from environment variables.
func NewSource() (*Source, error) {
	cfg, err := loadConfigFromEnv()
	if err != nil {
		return nil, handleErr(err)
	}

	c, err := newClient(cfg)
	if err != nil {
		return nil, handleErr(err)
	}

	return &Source{
		config: cfg,
		client: c,
	}, nil
}

// StartSyncProcess implements source.SyncableSource.
// It resolves repositories (one or all), then for each repository fans out
// component assets onto the results channel.
func (s *Source) StartSyncProcess(ctx context.Context, typesToSync map[string]source.Extra, results chan<- source.Data) error {
	log := logger.FromContext(ctx).WithName(loggerName)

	if !s.syncLock.TryLock() {
		log.Debug("sync process already running")
		return nil
	}
	defer s.syncLock.Unlock()

	_, syncComponentAssets := typesToSync[componentAssetType]

	// Log unknown types.
	for typeKey := range typesToSync {
		if typeKey != componentAssetType {
			log.Debug("unknown type, skipping", "type", typeKey)
		}
	}

	if !syncComponentAssets {
		log.Debug("no known types requested, nothing to do")
		return nil
	}

	// Resolve repositories.
	repos, err := s.resolveRepositories(ctx, log)
	if err != nil {
		return handleErr(err)
	}

	log.Trace("resolved repositories", "count", len(repos))

	// Iterate over each resolved repository.
	for _, repo := range repos {
		if err := ctx.Err(); err != nil {
			return nil
		}

		repoName, _ := repo["name"].(string)

		if err := s.syncComponentAssets(ctx, log, repoName, results); err != nil {
			log.Error("failed to sync component-assets for repository", "repository", repoName, "error", err)
			continue
		}
	}

	return nil
}

// resolveRepositories returns the list of repositories to iterate over.
// If SpecificRepository is set, it fetches just that one; otherwise lists all.
func (s *Source) resolveRepositories(ctx context.Context, log logger.Logger) ([]repositoriesResponse, error) {
	if s.config.SpecificRepository != "" {
		log.Trace("fetching specific repository", "repository", s.config.SpecificRepository)
		repo, err := s.client.getRepository(ctx, s.config.SpecificRepository)
		if err != nil {
			return nil, fmt.Errorf("failed to get repository %q: %w", s.config.SpecificRepository, err)
		}
		return []repositoriesResponse{repo}, nil
	}

	log.Trace("listing all repositories")
	repos, err := s.client.listRepositories(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list repositories: %w", err)
	}
	return repos, nil
}

// syncComponentAssets fetches all components for a repository with pagination,
// fans out each component's assets, and pushes one source.Data per asset.
func (s *Source) syncComponentAssets(ctx context.Context, log logger.Logger, repository string, results chan<- source.Data) error {
	continuationToken := ""
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}

		log.Trace("fetching components page", "repository", repository, "continuationToken", continuationToken)

		page, err := s.client.listComponentsPage(ctx, repository, continuationToken)
		if err != nil {
			return fmt.Errorf("failed to list components for repository %q: %w", repository, err)
		}

		for _, component := range page.Items {
			assets, _ := component["assets"].([]any)
			if len(assets) == 0 {
				continue
			}

			for _, rawAsset := range assets {
				asset, ok := rawAsset.(map[string]any)
				if !ok {
					continue
				}

				values := flattenComponentAsset(component, asset)
				results <- source.Data{
					Type:      componentAssetType,
					Operation: source.DataOperationUpsert,
					Values:    values,
					Time:      timeSource(),
				}
			}
		}

		if page.ContinuationToken == nil || *page.ContinuationToken == "" {
			break
		}
		continuationToken = *page.ContinuationToken
	}

	return nil
}

// flattenComponentAsset constructs the flattened map for a single component-asset entry.
// It copies component-level fields and adds a single "asset" key with the asset data.
func flattenComponentAsset(component, asset map[string]any) map[string]any {
	values := map[string]any{
		"id":         component["id"],
		"repository": component["repository"],
		"format":     component["format"],
		"group":      component["group"],
		"name":       component["name"],
		"version":    component["version"],
		"tags":       component["tags"],
		"asset":      asset,
	}
	return values
}

// handleErr wraps non-nil errors with ErrNexusSource, matching the project convention.
// Context cancellation errors are silently swallowed (return nil).
func handleErr(err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, context.Canceled) {
		return nil
	}

	return fmt.Errorf("%w: %w", ErrNexusSource, err)
}
