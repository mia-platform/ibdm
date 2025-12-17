// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package cmd

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/mia-platform/ibdm/internal/destination"
	"github.com/mia-platform/ibdm/internal/destination/catalog"
	"github.com/mia-platform/ibdm/internal/destination/writer"
)

const (
	mappingPathFlagName  = "mapping-file"
	mappingPathFlagShort = "f"
	mappingPathFlagUsage = "Path to a file or directory containing custom mapping rules. Can be specified multiple times."

	localOutputFlagName  = "local-output"
	localOutputFlagUsage = "If set, writes the output to stdout instead of sending it to the remote"
	defaultLocalOutput   = false
)

// flags collects the CLI options shared by the run and sync commands.
type flags struct {
	mappingPaths []string
	localOutput  bool
}

// addFlags registers the CLI flags on cmd.
func (f *flags) addFlags(cmd *cobra.Command) {
	cmd.Flags().StringArrayVarP(
		&f.mappingPaths,
		mappingPathFlagName,
		mappingPathFlagShort,
		nil,
		mappingPathFlagUsage)

	cmd.Flags().BoolVar(&f.localOutput, localOutputFlagName, defaultLocalOutput, localOutputFlagUsage)
}

// toOptions builds an options instance from the parsed flags and CLI arguments.
func (f *flags) toOptions(cmd *cobra.Command, args []string) (*options, error) {
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
		var err error
		destination, err = catalog.NewDestination()
		if err != nil {
			return nil, err
		}
	}

	return &options{
		integrationName: strings.ToLower(integrationName),
		mappingPaths:    mappingPaths,
		destination:     destination,
		sourceGetter:    sourceFromIntegrationName,
	}, nil
}
