// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package sonarqube

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/mia-platform/ibdm/internal/source"
)

const (
	loggerName = "ibdm:source:sonarqube"

	// issueType is the only data type supported by the SonarQube source.
	issueType = "issue"
)

var (
	// ErrSonarQubeSource wraps all errors originating from the SonarQube source.
	ErrSonarQubeSource = errors.New("sonarqube source")

	// timeSource is a package-level function for the current time, replaceable in tests.
	timeSource = time.Now
)

var _ source.SyncableSource = &Source{}
var _ source.WebhookSource = &Source{}

// Source implements [source.SyncableSource] and [source.WebhookSource] for
// SonarQube. It reads the issues of an analysis from the SonarQube Web API and
// pushes one [source.Data] per issue through the IBDM pipeline.
type Source struct {
	config        config
	webhookConfig webhookConfig
	scm           scmSettings
	client        *client

	syncLock sync.Mutex
}

// NewSource constructs a [Source] by reading its configuration from environment
// variables. It returns [ErrSonarQubeSource] if the configuration is invalid.
func NewSource() (*Source, error) {
	cfg, err := loadConfigFromEnv()
	if err != nil {
		return nil, handleErr(err)
	}

	whCfg, err := loadWebhookConfigFromEnv()
	if err != nil {
		return nil, handleErr(err)
	}

	scmProvider, err := parseProvider(cfg.SCMProvider)
	if err != nil {
		return nil, handleErr(err)
	}

	return &Source{
		config:        cfg,
		webhookConfig: whCfg,
		scm: scmSettings{
			provider:    scmProvider,
			urlTemplate: cfg.SCMURLTemplate,
		},
		client: newClient(cfg),
	}, nil
}

// resolveSCMTarget works out where the analysed code lives, once per analysis.
// Two sources are tried in order:
//
//  1. the sonar.analysis.repoUrl analysis property, injected by the CI job that
//     ran the scan: free, and the CI is the one that knows;
//  2. the project sonar.links.scm link, read from SonarQube.
func (s *Source) resolveSCMTarget(ctx context.Context, projectKey, propertyURL, revision, branch string) *scmTarget {
	if s.scm.provider == providerNone {
		return nil
	}

	repositoryURL := strings.TrimSpace(propertyURL)
	if repositoryURL == "" {
		repositoryURL = strings.TrimSpace(s.client.projectRepositoryURL(ctx, projectKey))
	}

	target, reason := newSCMTarget(repositoryURL, revision, branch, s.scm)
	if reason != "" {
		logger.FromContext(ctx).WithName(loggerName).Warn(reason, "project", projectKey)
	}
	return target
}

// emitAnalysis emits what one analysis produced, for the mapped types: its
// run, and one upsert per issue of the analysed ref, each carrying the run key
// its relationship to the run is built from. The run is sent first, so that
// the items the relationships point at are written before them.
//
// The issues are read only when they are mapped and readIssues is set: an
// analysis that did not succeed has none worth reading.
func (s *Source) emitAnalysis(ctx context.Context, analysis analysisContext, scope componentScope, run runInfo, readIssues bool, types map[string]source.Extra, eventTime time.Time, results chan<- source.Data) error {
	log := logger.FromContext(ctx).WithName(loggerName)
	_, issuesMapped := types[issueType]
	_, runsMapped := types[runType]

	if runsMapped {
		analysis.runKey = runKey(analysis)
		if analysis.runKey == "" {
			log.Warn("the analysis carries neither a date nor a task id; its run is not recorded", "project", analysis.projectKey)
		}
	}

	var fetched *analysisIssues
	var readErr error
	if issuesMapped && readIssues {
		fetched, readErr = s.client.issues(ctx, analysis.projectKey, scope)
		if readErr == nil {
			if fetched.truncated {
				log.Warn("the read of this analysis stopped short of everything SonarQube holds", "project", analysis.projectKey, "issuesRead", len(fetched.issues))
			}
			log.Debug("issues read", "project", analysis.projectKey, "branch", analysis.branch, "pullRequest", analysis.pullRequest, "count", len(fetched.issues))
		}
	}

	// The run happened whether or not its issues could be read: it is
	// recorded, without counts when they are unknown, and a read error is
	// still reported below.
	if analysis.runKey != "" {
		if err := send(ctx, results, runType, runValues(analysis, run, fetched), eventTime); err != nil {
			return err
		}
	}

	if readErr != nil {
		return readErr
	}
	if fetched == nil {
		return nil
	}
	for _, issue := range fetched.issues {
		if err := send(ctx, results, issueType, issueValues(issue, fetched.rules, analysis), eventTime); err != nil {
			return err
		}
	}

	return nil
}

// send pushes one upsert onto results, giving up when ctx is done.
func send(ctx context.Context, results chan<- source.Data, dataType string, values map[string]any, eventTime time.Time) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case results <- source.Data{
		Type:      dataType,
		Operation: source.DataOperationUpsert,
		Values:    values,
		Time:      eventTime,
	}:
		return nil
	}
}

// mapsAny reports whether types asks for at least one type this source emits.
func mapsAny(types map[string]source.Extra) bool {
	_, issues := types[issueType]
	_, runs := types[runType]
	return issues || runs
}

// publicURL returns the address a person opens SonarQube at: the configured
// public URL, else fallback.
func (s *Source) publicURL(fallback string) string {
	if s.config.PublicURL != "" {
		return s.config.PublicURL
	}
	return strings.TrimRight(fallback, "/")
}

// handleErr wraps non-nil errors with ErrSonarQubeSource. Context
// cancellation is not an error for a source.
func handleErr(err error) error {
	if err == nil || errors.Is(err, context.Canceled) {
		return nil
	}

	return fmt.Errorf("%w: %w", ErrSonarQubeSource, err)
}
