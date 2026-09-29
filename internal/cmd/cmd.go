// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package cmd

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/spf13/cobra"
)

const (
	runCmdUsageTemplate = "run [%s]"
	runCmdShort         = "start an event stream specific integration by name"
	runCmdLong          = `Start an event stream specific integration by name.
	Every integration can expose a webhook or start a polling mechanism to receive
	data events and have its own configuration options, please refer to the
	documentation for more details.
	`

	runCmdExample = `# Run the Mia Platform Console integration with every internal mapping
	ibdm run console --include-internal-mappings=all

	# Run the GitHub integration with two internal mappings and external ones
	ibdm run github --include-internal-mappings=repositories,workflowruns --mapping-file ./my-mappings/`

	syncCmdUsageTemplate = "sync [%s]"
	syncCmdShort         = "start a sync specific integration by name"
	syncCmdLong          = `Start a sync specific integration by name.
	The synchronization process runs once and uses the syncable mappings only:
	a mapping declaring 'syncable: false' is skipped.
	`

	syncCmdExample = `# Sync every internal Azure mapping except one
	ibdm sync azure --include-internal-mappings=all --exclude-internal-mappings=virtualmachines

	# Sync the GitLab projects and pipelines only
	ibdm sync gitlab --include-internal-mappings=projects,pipelines`

	mappingsSelectionHelp = `
	The mappings used are the internal mappings selected with --include-internal-mappings,
	minus the ones listed in --exclude-internal-mappings, plus every external mapping passed
	with --mapping-file. List the internal mappings with 'ibdm mappings list <integration>'.
	With no mapping selected, the command exits without doing anything.

	The available integrations are:
	`
)

// integrationsHelp lists the integrations of sources, one per line, in lexical order.
func integrationsHelp(sources map[string]string) string {
	lines := make([]string, 0, len(sources))
	for _, name := range slices.Sorted(maps.Keys(sources)) {
		lines = append(lines, fmt.Sprintf("- %s: %s", name, sources[name]))
	}
	return strings.Join(lines, "\n")
}

// RunCmd returns the Cobra command that starts an event-stream integration.
func RunCmd() *cobra.Command {
	flags := &flags{}
	allSources := slices.Sorted(maps.Keys(availableEventSources))
	cmd := &cobra.Command{
		Use:     fmt.Sprintf(runCmdUsageTemplate, strings.Join(allSources, "|")),
		Short:   heredoc.Doc(runCmdShort),
		Long:    heredoc.Doc(runCmdLong+mappingsSelectionHelp) + integrationsHelp(availableEventSources),
		Example: heredoc.Doc(runCmdExample),

		SilenceErrors: true,
		SilenceUsage:  true,

		ValidArgsFunction: validArgsFunc(availableEventSources),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts, err := flags.toOptions(cmd, args)
			if err != nil {
				return handleError(cmd, err)
			}

			if err := opts.validate(availableEventSources); err != nil {
				return handleError(cmd, err)
			}

			if err := opts.executeEventStream(cmd.Context()); err != nil {
				return handleError(cmd, err)
			}

			return nil
		},
	}

	flags.addFlags(cmd)
	return cmd
}

// SyncCmd returns the Cobra command that starts a sync integration.
func SyncCmd() *cobra.Command {
	flags := &flags{}

	allSources := slices.Sorted(maps.Keys(availableSyncSources))
	cmd := &cobra.Command{
		Use:     fmt.Sprintf(syncCmdUsageTemplate, strings.Join(allSources, "|")),
		Short:   heredoc.Doc(syncCmdShort),
		Long:    heredoc.Doc(syncCmdLong+mappingsSelectionHelp) + integrationsHelp(availableSyncSources),
		Example: heredoc.Doc(syncCmdExample),

		SilenceErrors: true,
		SilenceUsage:  true,

		ValidArgsFunction: validArgsFunc(availableSyncSources),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts, err := flags.toOptions(cmd, args)
			if err != nil {
				return handleError(cmd, err)
			}

			if err := opts.validate(availableSyncSources); err != nil {
				return handleError(cmd, err)
			}

			if err := opts.executeSync(cmd.Context()); err != nil {
				return handleError(cmd, err)
			}

			return nil
		},
	}

	flags.addFlags(cmd)
	return cmd
}
