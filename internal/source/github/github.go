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

	if _, ok := typesToSync[repositoryType]; ok {
		if err := s.syncRepositories(ctx, typesToSync[repositoryType], results); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil
			}
			log.Error("error syncing repositories", "error", err.Error())
			return fmt.Errorf("%w: %w", ErrGitHubSource, err)
		}
	}

	for dataType := range typesToSync {
		if dataType != repositoryType {
			log.Debug("skipping unknown data type", "type", dataType)
		}
	}

	return nil
}

// syncRepositories fetches all repositories for the configured organization
// and pushes each as a source.Data entry onto the results channel.
func (s *Source) syncRepositories(ctx context.Context, extra source.Extra, results chan<- source.Data) error {
	apiVersion := apiVersionFromExtra(extra)
	it := s.client.listRepositories(apiVersion)
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
			results <- source.Data{
				Type:      repositoryType,
				Operation: source.DataOperationUpsert,
				Values:    map[string]any{repositoryType: item},
				Time:      timeSource(),
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
