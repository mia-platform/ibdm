// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package cmd

import (
	"context"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/spf13/cobra"
)

const (
	runCmdUsage = "run INTEGRATION"
	runCmdShort = "start a specific integration by name"
	runCmdLong  = `Start a specific integration by name.
	Every integration can expose a webhook or start a polling mechanism to receive
	data events and have its own configuration options, please refer to the
	documentation for more details.

	The available integrations are:
	- gcp: Google Cloud Platform integration`

	runCmdExample = `# Run the Google Cloud Platform integration
	ibdm run gcp`
)

// RunCmd return the "run" cli command for starting an integration.
func RunCmd() *cobra.Command {
	flags := &runFlags{}
	cmd := &cobra.Command{
		Use:     runCmdUsage,
		Short:   heredoc.Doc(runCmdShort),
		Long:    heredoc.Doc(runCmdLong),
		Example: heredoc.Doc(runCmdExample),

		ValidArgsFunction: cobra.FixedCompletions([]cobra.Completion{
			cobra.CompletionWithDesc("gcp", "Google Cloud Platform integration"),
		}, cobra.ShellCompDirectiveNoFileComp),

		RunE: func(cmd *cobra.Command, args []string) error {
			opts := flags.toOptions(args)
			if err := opts.validate(); err != nil {
				return handleError(cmd, err)
			}

			if err := runIntegration(cmd.Context(), opts); err != nil {
				return handleError(cmd, err)
			}

			return nil
		},
	}

	flags.addFlags(cmd)
	return cmd
}

// runIntegration starts the specified integration.
func runIntegration(_ context.Context, _ *runOptions) error {
	return nil
}
