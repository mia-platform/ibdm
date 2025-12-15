// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package cmd

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/mia-platform/ibdm/internal/destination"
	"github.com/mia-platform/ibdm/internal/destination/writer"
	"github.com/mia-platform/ibdm/internal/pipeline"
)

// syncFlags holds the flags for the "sync" command.
type syncFlags struct {
	mappingPaths []string
	localOutput  bool
}

// addFlags adds the cli flags to the cobra command.
func (f *syncFlags) addFlags(cmd *cobra.Command) {
	cmd.Flags().StringArrayVarP(
		&f.mappingPaths,
		mappingPathFlagName,
		mappingPathFlagShort,
		nil,
		mappingPathFlagUsage)

	cmd.Flags().BoolVar(&f.localOutput, localOutputFlagName, defaultLocalOutput, localOutputFlagUsage)
}

// toOptions converts the sync flags to syncOptions enriching it with the passed arguments.
func (f *syncFlags) toOptions(cmd *cobra.Command, args []string) (*syncOptions, error) {
	integrationName := ""
	if len(args) > 0 {
		integrationName = args[0]
	}

	mappingPaths, err := collectPaths(f.mappingPaths)
	if err != nil {
		return nil, err
	}

	var destination destination.Sender
	if f.localOutput {
		destination = writer.NewDestination(cmd.OutOrStdout())
	} else {
		// TODO: implement remote destination
		destination = nil
	}

	return &syncOptions{
		integrationName: strings.ToLower(integrationName),
		mappingPaths:    mappingPaths,
		destination:     destination,
	}, nil
}

type syncOptions struct {
	integrationName string
	mappingPaths    []string
	destination     destination.Sender

	lock sync.Mutex
}

// validate validates the sync options and returns an error if something is wrong.
func (o *syncOptions) validate() error {
	if o.integrationName == "" {
		return errNoArguments
	}

	if _, ok := availableSyncSources[o.integrationName]; !ok {
		return fmt.Errorf("%w: %s", errInvalidIntegration, o.integrationName)
	}

	return nil
}

// execute starts a data pipeline based on the sync options.
func (o *syncOptions) execute(ctx context.Context) error {
	if !o.lock.TryLock() {
		return nil
	}
	defer o.lock.Unlock()

	mappers, err := loadMappers(o.mappingPaths, false)
	if err != nil {
		return err
	}

	source, err := sourceGetter(o.integrationName)
	if err != nil {
		return err
	}

	pipeline := pipeline.New(source, mappers, o.destination)
	return pipeline.Sync(ctx)
}
