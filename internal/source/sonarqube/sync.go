// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package sonarqube

import (
	"context"
	"errors"

	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/mia-platform/ibdm/internal/source"
)

// StartSyncProcess implements [source.SyncableSource]. For the main branch of
// every project the token can browse, or of the projects listed in
// SONARQUBE_PROJECT_KEYS, it emits the run of the latest analysis and one
// upsert per current issue.
func (s *Source) StartSyncProcess(ctx context.Context, typesToSync map[string]source.Extra, results chan<- source.Data) error {
	log := logger.FromContext(ctx).WithName(loggerName)
	if !s.syncLock.TryLock() {
		log.Debug("sync process already running")
		return nil
	}
	defer s.syncLock.Unlock()

	for dataType := range typesToSync {
		if dataType != issueType && dataType != runType {
			log.Debug("skipping unknown data type", "type", dataType)
		}
	}
	if !mapsAny(typesToSync) {
		log.Debug("no known types requested, nothing to do")
		return nil
	}

	projects, err := s.resolveProjects(ctx)
	if err != nil {
		return handleErr(err)
	}
	log.Trace("resolved projects", "count", len(projects))

	for _, p := range projects {
		if ctx.Err() != nil {
			return nil
		}

		if err := s.syncProject(ctx, p, typesToSync, results); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil
			}
			log.Error("error syncing project", "project", p.Key, "error", err.Error())
			continue
		}
	}

	return nil
}

// resolveProjects returns the projects to sync: the configured ones, or every
// project the token can browse.
func (s *Source) resolveProjects(ctx context.Context) ([]project, error) {
	if len(s.config.ProjectKeys) == 0 {
		return s.client.listProjects(ctx)
	}

	log := logger.FromContext(ctx).WithName(loggerName)
	projects := make([]project, 0, len(s.config.ProjectKeys))
	for _, key := range s.config.ProjectKeys {
		p, err := s.client.getProject(ctx, key)
		if err != nil {
			// Still synced: the name is the only thing lost.
			log.Warn("reading the project failed; syncing it without its name", "project", key, "error", err.Error())
			p = project{Key: key}
		}
		if p.Key == "" {
			p.Key = key
		}
		projects = append(projects, p)
	}
	return projects, nil
}

// syncProject emits the latest run and the issues of the main branch of one project.
//
// The issues are read without a branch parameter, which SonarQube answers for
// the main branch; its name is looked up so that the items carry the same
// branch as those written by a webhook delivery for it. The latest analysis
// gives the date and revision a webhook delivery would have carried, so the
// run is the same item a webhook writes, and SCM links point at the commit.
func (s *Source) syncProject(ctx context.Context, p project, types map[string]source.Extra, results chan<- source.Data) error {
	log := logger.FromContext(ctx).WithName(loggerName)

	analysis := analysisContext{
		serverURL:   s.publicURL(s.config.URL),
		projectKey:  p.Key,
		projectName: p.Name,
	}

	main, err := s.client.mainBranch(ctx, p.Key)
	if err != nil {
		log.Debug("could not resolve the main branch; items carry no branch", "project", p.Key, "error", err.Error())
	} else {
		isMain := true
		analysis.branch = main.Name
		analysis.isMainBranch = &isMain
		analysis.analysedAt = rfc3339(main.AnalysisDate)
	}

	run := runInfo{status: statusSuccess}
	if latest, err := s.client.latestAnalysis(ctx, p.Key); err != nil {
		// The main branch analysis date still identifies the run.
		log.Debug("could not read the latest analysis; the run carries no revision", "project", p.Key, "error", err.Error())
	} else {
		analysis.analysedAt = rfc3339(latest.Date)
		analysis.revision = latest.Revision
		run.analysisKey = latest.Key
		run.projectVersion = latest.ProjectVersion
		s.addQualityGate(ctx, &analysis, &run)
	}

	// Without a revision the SCM link can only point at the branch.
	analysis.scm = s.resolveSCMTarget(ctx, p.Key, "", analysis.revision, analysis.branch)

	return s.emitAnalysis(ctx, analysis, componentScope{}, run, true, types, timeSource(), results)
}

// addQualityGate fills in the quality gate verdict on the analysis run.
// Best effort: a token that cannot read it costs those fields, not the sync.
func (s *Source) addQualityGate(ctx context.Context, analysis *analysisContext, run *runInfo) {
	log := logger.FromContext(ctx).WithName(loggerName)

	gate, err := s.client.analysisQualityGate(ctx, run.analysisKey)
	if err != nil {
		log.Debug("could not read the quality gate status", "project", analysis.projectKey, "error", err.Error())
		return
	}
	// NONE is what SonarQube answers for a project without a quality gate.
	if gate.Status != "" && gate.Status != "NONE" {
		analysis.qualityGateStatus = gate.Status
	}
	run.conditions = gate.Conditions

	if name, err := s.client.qualityGateName(ctx, analysis.projectKey); err != nil {
		log.Debug("could not read the quality gate name", "project", analysis.projectKey, "error", err.Error())
	} else {
		analysis.qualityGateName = name
	}
}
