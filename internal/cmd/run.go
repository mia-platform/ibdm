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
	runCmdShort         = "start a specific integration by name"
	runCmdLong          = `Start a specific integration by name.
	Every integration can expose a webhook or start a polling mechanism to receive
	data events and have its own configuration options, please refer to the
	documentation for more details.

	The available integrations are:
	- gcp: Google Cloud Platform integration`

	runCmdExample = `# Run the Google Cloud Platform integration
	ibdm run gcp`
	// runLoggerName = "ibdm:run"
)

// RunCmd return the "run" cli command for starting an integration.
func RunCmd() *cobra.Command {
	flags := &runFlags{}
	allSources := slices.Sorted(maps.Keys(availableEventSources))
	cmd := &cobra.Command{
		Use:     fmt.Sprintf(runCmdUsageTemplate, strings.Join(allSources, "|")),
		Short:   heredoc.Doc(runCmdShort),
		Long:    heredoc.Doc(runCmdLong),
		Example: heredoc.Doc(runCmdExample),

		SilenceErrors: true,
		SilenceUsage:  true,

		ValidArgsFunction: validArgsFunc(availableEventSources),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts, err := flags.toOptions(cmd, args)
			if err != nil {
				return handleError(cmd, err)
			}

			if err := opts.validate(); err != nil {
				return handleError(cmd, err)
			}

			if err := opts.execute(cmd.Context()); err != nil {
				return handleError(cmd, err)
			}

			return nil
		},
	}

	flags.addFlags(cmd)
	return cmd
}
