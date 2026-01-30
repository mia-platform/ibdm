// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package cmd

import (
	"context"
	"fmt"
	"sync"

	"github.com/mia-platform/ibdm/internal/destination"
	"github.com/mia-platform/ibdm/internal/pipeline"
)

// options configures pipelines for event streams and sync runs.
type options struct {
	integrationName string
	mappingPaths    []string
	destination     destination.Sender
	sourceGetter    func(string) (any, error)

	lock sync.Mutex
}

// validate checks the configured values and reports invalid setups.
func (o *options) validate() error {
	if o.integrationName == "" {
		return errNoArguments
	}

	if _, ok := availableEventSources[o.integrationName]; !ok {
		return fmt.Errorf("%w: %s", errInvalidIntegration, o.integrationName)
	}

	return nil
}

// executeEventStream starts the event stream pipeline configured by the options.
func (o *options) executeEventStream(ctx context.Context) error {
	if !o.lock.TryLock() {
		return nil
	}
	defer o.lock.Unlock()

	pipeline, err := o.pipeline(ctx)
	if err != nil {
		return err
	}

	return pipeline.Start(ctx)
}

// executeSync launches the sync pipeline configured by the options.
func (o *options) executeSync(ctx context.Context) error {
	if !o.lock.TryLock() {
		return nil
	}
	defer o.lock.Unlock()

	pipeline, err := o.pipeline(ctx)
	if err != nil {
		return err
	}

	return pipeline.Sync(ctx)
}

// pipeline assembles a pipeline from the configured source, mappers, and destination.
func (o *options) pipeline(ctx context.Context) (*pipeline.Pipeline, error) {
	mappers, err := loadMappers(o.mappingPaths, false)
	if err != nil {
		return nil, err
	}

	source, err := o.sourceGetter(o.integrationName)
	if err != nil {
		return nil, err
	}

	return pipeline.New(ctx, source, mappers, o.destination)
}
