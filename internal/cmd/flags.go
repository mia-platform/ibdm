// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package cmd

import (
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mia-platform/ibdm/internal/destination"
	"github.com/mia-platform/ibdm/internal/destination/catalog"
	"github.com/mia-platform/ibdm/internal/destination/writer"
	"github.com/mia-platform/ibdm/internal/mappings"
)

const (
	mappingFileFlagName  = "mapping-file"
	mappingFileFlagShort = "f"
	mappingFileFlagUsage = "Path to a file or directory containing external mapping rules. Can be specified multiple times. " +
		"External mappings are always loaded in full, in addition to the selected internal mappings: " +
		"the internal selection flags never narrow them."

	includeInternalMappingsFlagName  = "include-internal-mappings"
	includeInternalMappingsFlagUsage = "Internal mappings of the integration to use, by name, or 'all' for every one of them. " +
		"See 'ibdm mappings list <integration>' for the names. Some mappings depend on others, so a partial " +
		"selection can produce fewer items, or none: see the integration documentation."

	excludeInternalMappingsFlagName  = "exclude-internal-mappings"
	excludeInternalMappingsFlagUsage = "Internal mappings to remove from the ones selected with --" + includeInternalMappingsFlagName +
		", by name. Excluding a mapping does not delete the items it already published."

	allowSharedItemTypesFlagName  = "allow-shared-item-types"
	allowSharedItemTypesFlagUsage = "Start even when several mappings write the same item type, whose items may overwrite each other."

	localOutputFlagName  = "local-output"
	localOutputFlagUsage = "If set, writes the output to stdout instead of sending it to the remote"
	defaultLocalOutput   = false
)

// flags collects the CLI options shared by the run and sync commands.
type flags struct {
	mappingFiles            []string
	includeInternalMappings []string
	excludeInternalMappings []string
	allowSharedItemTypes    bool
	localOutput             bool
}

// addFlags registers the CLI flags on cmd.
func (f *flags) addFlags(cmd *cobra.Command) {
	cmd.Flags().StringArrayVarP(
		&f.mappingFiles,
		mappingFileFlagName,
		mappingFileFlagShort,
		nil,
		mappingFileFlagUsage)

	cmd.Flags().StringSliceVar(&f.includeInternalMappings, includeInternalMappingsFlagName, nil, includeInternalMappingsFlagUsage)
	cmd.Flags().StringSliceVar(&f.excludeInternalMappings, excludeInternalMappingsFlagName, nil, excludeInternalMappingsFlagUsage)
	cmd.Flags().BoolVar(&f.allowSharedItemTypes, allowSharedItemTypesFlagName, false, allowSharedItemTypesFlagUsage)
	cmd.Flags().BoolVar(&f.localOutput, localOutputFlagName, defaultLocalOutput, localOutputFlagUsage)

	_ = cmd.RegisterFlagCompletionFunc(includeInternalMappingsFlagName, internalMappingsCompletion(true))
	_ = cmd.RegisterFlagCompletionFunc(excludeInternalMappingsFlagName, internalMappingsCompletion(false))
}

// internalMappingsCompletion completes the last element of a comma separated list of internal
// mapping names of the integration given as first argument. withAll also offers 'all'.
func internalMappingsCompletion(withAll bool) cobra.CompletionFunc {
	return func(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		names, err := mappings.Names(strings.ToLower(args[0]))
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		if withAll && !strings.Contains(toComplete, ",") {
			names = append([]string{allMappingsSelector}, names...)
		}

		prefix := ""
		chosen := []string{}
		if index := strings.LastIndex(toComplete, ","); index >= 0 {
			prefix = toComplete[:index+1]
			chosen = strings.Split(toComplete[:index], ",")
		}

		completions := make([]string, 0, len(names))
		for _, name := range names {
			if !slices.Contains(chosen, name) && strings.HasPrefix(prefix+name, toComplete) {
				completions = append(completions, prefix+name)
			}
		}
		return completions, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace
	}
}

// toOptions builds an options instance from the parsed flags and CLI arguments.
func (f *flags) toOptions(cmd *cobra.Command, args []string) (*options, error) {
	integrationName := ""
	if len(args) > 0 {
		integrationName = args[0]
	}

	mappingPaths, err := collectPaths(f.mappingFiles)
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
		selection: internalSelection{
			include:    f.includeInternalMappings,
			exclude:    f.excludeInternalMappings,
			includeSet: cmd.Flags().Changed(includeInternalMappingsFlagName),
			excludeSet: cmd.Flags().Changed(excludeInternalMappingsFlagName),
		},
		allowSharedItemTypes: f.allowSharedItemTypes,
		destination:          destination,
		sourceGetter:         sourceFromIntegrationName,
		internalMappings:     mappings.ForSource,
	}, nil
}
