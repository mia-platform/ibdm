// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package jenkins

import (
	"context"
	"errors"
	"fmt"

	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/mia-platform/ibdm/internal/source"
)

const (
	instanceType      = "instance"
	folderType        = "folder"
	multibranchType   = "multibranch"
	workflowType      = "workflow"
	sharedLibraryType = "sharedlibrary"
	credentialType    = "credential"
	pluginType        = "plugin"
)

// knownTypes lists every data type the source can emit.
var knownTypes = map[string]struct{}{
	instanceType:      {},
	folderType:        {},
	multibranchType:   {},
	workflowType:      {},
	sharedLibraryType: {},
	credentialType:    {},
	pluginType:        {},
}

// StartSyncProcess implements source.SyncableSource. It emits the requested data types, walking
// the folder tree once for every type that needs it. A failure of one step does not stop the
// others: the failures are returned together once every step has run.
func (s *Source) StartSyncProcess(ctx context.Context, typesToSync map[string]source.MappingExtras, results chan<- source.Data) error {
	log := logger.FromContext(ctx).WithName(loggerName)

	if !s.syncLock.TryLock() {
		log.Debug("sync process already running")
		return nil
	}
	defer s.syncLock.Unlock()

	for dataType := range typesToSync {
		if _, ok := knownTypes[dataType]; !ok {
			log.Debug("skipping unknown data type", "type", dataType)
		}
	}

	wants := func(dataType string) bool {
		_, ok := typesToSync[dataType]
		return ok
	}

	log.Trace("starting sync")
	var errs []error
	if wants(instanceType) {
		if err := s.syncInstance(ctx, results); err != nil {
			errs = append(errs, fmt.Errorf("instance: %w", err))
		}
	}

	if ctx.Err() == nil {
		errs = append(errs, s.syncFolderTree(ctx, wants, results)...)
	}

	if wants(pluginType) && ctx.Err() == nil {
		if err := s.syncPlugins(ctx, results); err != nil {
			errs = append(errs, fmt.Errorf("plugins: %w", err))
		}
	}

	if ctx.Err() != nil {
		return nil
	}

	log.Trace("sync completed", "failedSteps", len(errs))
	return handleErr(errors.Join(errs...))
}

// syncFolderTree walks the folder tree when a requested type needs it, then emits the shared
// libraries and the credentials, which depend on what the walk collected.
func (s *Source) syncFolderTree(ctx context.Context, wants func(string) bool, results chan<- source.Data) []error {
	opts := walkOptions{
		emitFolders:      wants(folderType),
		emitMultibranch:  wants(multibranchType),
		emitWorkflows:    wants(workflowType),
		collectLibraries: wants(sharedLibraryType),
	}
	if !opts.emitFolders && !opts.emitMultibranch && !opts.emitWorkflows && !opts.collectLibraries && !wants(credentialType) {
		return nil
	}

	inv, err := s.walkJobs(ctx, opts, results)
	if err != nil {
		return []error{fmt.Errorf("folder tree: %w", err)}
	}

	var errs []error
	if opts.collectLibraries {
		for _, values := range inv.libraries.values() {
			if err := send(ctx, results, sharedLibraryType, values); err != nil {
				return append(errs, err)
			}
		}
	}

	if wants(credentialType) && ctx.Err() == nil {
		if err := s.syncCredentials(ctx, inv, results); err != nil {
			errs = append(errs, fmt.Errorf("credentials: %w", err))
		}
	}

	return errs
}
