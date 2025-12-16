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

// options implement the logic for building and starting a new data pipeline for sync or event
// stream processes.
type options struct {
	integrationName string
	mappingPaths    []string
	destination     destination.Sender
	sourceGetter    func(string) (any, error)

	lock sync.Mutex
}

// validate check the options parameters and returns an error if something is wrong.
func (o *options) validate() error {
	if o.integrationName == "" {
		return errNoArguments
	}

	if _, ok := availableEventSources[o.integrationName]; !ok {
		return fmt.Errorf("%w: %s", errInvalidIntegration, o.integrationName)
	}

	return nil
}

// executeEventStream starts a data pipeline event stream based on the run options.
func (o *options) executeEventStream(ctx context.Context) error {
	if !o.lock.TryLock() {
		return nil
	}
	defer o.lock.Unlock()

	pipeline, err := o.pipeline()
	if err != nil {
		return err
	}

	return pipeline.Start(ctx)
}

// executeSync starts a data pipeline sync process based on the run options.
func (o *options) executeSync(ctx context.Context) error {
	if !o.lock.TryLock() {
		return nil
	}
	defer o.lock.Unlock()

	pipeline, err := o.pipeline()
	if err != nil {
		return err
	}

	return pipeline.Sync(ctx)
}

// pipeline builds a new data pipeline based on the run options.
func (o *options) pipeline() (*pipeline.Pipeline, error) {
	mappers, err := loadMappers(o.mappingPaths, false)
	if err != nil {
		return nil, err
	}

	source, err := o.sourceGetter(o.integrationName)
	if err != nil {
		return nil, err
	}

	return pipeline.New(source, mappers, o.destination), nil
}
