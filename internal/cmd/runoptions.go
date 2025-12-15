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

const (
	mappingPathFlagName  = "mapping-file"
	mappingPathFlagShort = "f"
	mappingPathFlagUsage = "Path to a file or directory containing custom mapping rules. Can be specified multiple times."

	localOutputFlagName  = "local-output"
	localOutputFlagUsage = "If set, writes the output to stdout instead of sending it to the remote"
	defaultLocalOutput   = false
)

// runFlags holds the flags for the "run" command.
type runFlags struct {
	mappingPaths []string
	localOutput  bool
}

// addFlags adds the cli flags to the cobra command.
func (f *runFlags) addFlags(cmd *cobra.Command) {
	cmd.Flags().StringArrayVarP(
		&f.mappingPaths,
		mappingPathFlagName,
		mappingPathFlagShort,
		nil,
		mappingPathFlagUsage)

	cmd.Flags().BoolVar(&f.localOutput, localOutputFlagName, defaultLocalOutput, localOutputFlagUsage)
}

// toOptions converts the run flags to runOptions enriching it with the passed arguments.
func (f *runFlags) toOptions(cmd *cobra.Command, args []string) (*runOptions, error) {
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

	return &runOptions{
		integrationName: strings.ToLower(integrationName),
		mappingPaths:    mappingPaths,
		destination:     destination,
	}, nil
}

// runOptions holds the options set for the current run function.
type runOptions struct {
	integrationName string
	mappingPaths    []string
	destination     destination.Sender

	lock sync.Mutex
}

// validate validates the run options and returns an error if something is wrong.
func (o *runOptions) validate() error {
	if o.integrationName == "" {
		return errNoArguments
	}

	if _, ok := availableEventSources[o.integrationName]; !ok {
		return fmt.Errorf("%w: %s", errInvalidIntegration, o.integrationName)
	}

	return nil
}

// execute starts a data pipeline based on the run options.
func (o *runOptions) execute(ctx context.Context) error {
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
	return pipeline.Start(ctx)
}
