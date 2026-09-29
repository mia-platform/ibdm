// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package cmd

import (
	"fmt"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/spf13/cobra"

	"github.com/mia-platform/ibdm/internal/mappings"
)

const (
	mappingsCmdShort = "inspect the internal mappings shipped with ibdm"

	mappingsListCmdShort = "list the internal mappings of an integration"
	mappingsListCmdLong  = `List, one per line, the names of the internal mappings shipped for an integration.
	Use them as values of --include-internal-mappings and --exclude-internal-mappings.`
	mappingsListCmdExample = `# List the internal mappings of the Mia Platform Console integration
	ibdm mappings list console`
)

// MappingsCmd returns the Cobra command grouping the internal mapping subcommands.
func MappingsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mappings",
		Short: heredoc.Doc(mappingsCmdShort),
	}

	cmd.AddCommand(mappingsListCmd())
	return cmd
}

// mappingsListCmd returns the command printing the internal mapping names of an integration.
func mappingsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     fmt.Sprintf("list [%s]", strings.Join(mappings.Sources(), "|")),
		Short:   heredoc.Doc(mappingsListCmdShort),
		Long:    heredoc.Doc(mappingsListCmdLong),
		Example: heredoc.Doc(mappingsListCmdExample),

		SilenceErrors: true,
		SilenceUsage:  true,

		ValidArgsFunction: cobra.FixedCompletions(mappings.Sources(), cobra.ShellCompDirectiveNoFileComp),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return handleError(cmd, errNoArguments)
			}

			names, err := mappings.Names(strings.ToLower(args[0]))
			if err != nil {
				return handleError(cmd, err)
			}

			for _, name := range names {
				fmt.Fprintln(cmd.OutOrStdout(), name)
			}
			return nil
		},
	}
}
