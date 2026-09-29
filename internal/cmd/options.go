// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package cmd

import (
	"context"
	"fmt"
	"sync"

	"github.com/mia-platform/ibdm/internal/config"
	"github.com/mia-platform/ibdm/internal/destination"
	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/mia-platform/ibdm/internal/pipeline"
)

const loggerName = "ibdm:cmd"

// options configures pipelines for event streams and sync runs.
type options struct {
	integrationName      string
	mappingPaths         []string
	selection            internalSelection
	allowSharedItemTypes bool
	destination          destination.Sender
	sourceGetter         func(string) (any, error)
	internalMappings     func(string) ([]*config.MappingConfig, error)

	lock sync.Mutex
}

// validate checks the configured values and reports invalid setups.
func (o *options) validate(eventSources map[string]string) error {
	if o.integrationName == "" {
		return errNoArguments
	}

	if _, ok := eventSources[o.integrationName]; !ok {
		return fmt.Errorf("%w: %s", errInvalidIntegration, o.integrationName)
	}

	return nil
}

// executeEventStream starts the event stream pipeline configured by the options. With no
// mapping selected it returns without starting anything.
func (o *options) executeEventStream(ctx context.Context) error {
	if !o.lock.TryLock() {
		return nil
	}
	defer o.lock.Unlock()

	pipeline, err := o.pipeline(ctx, false)
	if err != nil || pipeline == nil {
		return err
	}

	return pipeline.Start(ctx)
}

// executeSync launches the sync pipeline configured by the options, with the syncable mappings
// only. With no mapping left it returns without doing anything.
func (o *options) executeSync(ctx context.Context) error {
	if !o.lock.TryLock() {
		return nil
	}
	defer o.lock.Unlock()

	pipeline, err := o.pipeline(ctx, true)
	if err != nil || pipeline == nil {
		return err
	}

	return pipeline.Sync(ctx)
}

// pipeline assembles a pipeline from the configured source, mappers, and destination. It returns
// a nil pipeline, and builds no source, when no mapping is selected.
func (o *options) pipeline(ctx context.Context, syncOnly bool) (*pipeline.Pipeline, error) {
	mappers, err := resolveMappers(logger.FromContext(ctx).WithName(loggerName), mapperRequest{
		source:               o.integrationName,
		selection:            o.selection,
		externalPaths:        o.mappingPaths,
		syncOnly:             syncOnly,
		allowSharedItemTypes: o.allowSharedItemTypes,
		internalMappings:     o.internalMappings,
	})
	if err != nil || len(mappers) == 0 {
		return nil, err
	}

	source, err := o.sourceGetter(o.integrationName)
	if err != nil {
		return nil, err
	}

	return pipeline.New(ctx, source, mappers, o.destination)
}
