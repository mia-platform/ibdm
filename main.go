// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package main

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/spf13/cobra"

	internalcmd "github.com/mia-platform/ibdm/internal/cmd"
	"github.com/mia-platform/ibdm/internal/info"
	"github.com/mia-platform/ibdm/internal/logger"
)

var (
	// Version is injected at build time via the Makefile.
	Version = info.Version
	// BuildDate is injected at build time via the Makefile.
	BuildDate = info.BuildDate

	appName      = info.AppName
	versionShort = "Display the " + appName + " version"
)

const (
	appShort = "ibdm is the CLI tool to create a Mia-Platform Catalog connector"

	logLevelFlagName      = "log-level"
	logLevelShortFlagName = "v"

	versionCmdName = "version"
)

var (
	allLoggerLevels = []string{
		logger.TRACE.String(),
		logger.DEBUG.String(),
		logger.INFO.String(),
		logger.WARN.String(),
		logger.ERROR.String(),
	}
	logLevelDefaultValue = logger.INFO.String()
	logLevelFlagUsage    = "set the logging level (possible values: " + strings.Join(allLoggerLevels, ", ") + ")"
)

// rootFlags holds the persistent flags shared across the command tree.
type rootFlags struct {
	logLevel string
}

// addFlags registers the persistent CLI flags on cmd.
func (f *rootFlags) addFlags(cmd *cobra.Command) {
	flags := cmd.PersistentFlags()
	flags.StringVarP(&f.logLevel, logLevelFlagName, logLevelShortFlagName, logLevelDefaultValue, heredoc.Doc(logLevelFlagUsage))
}

func main() {
	cmd := rootCmd()
	log := logger.NewLogger(cmd.OutOrStderr())
	ctx := logger.WithContext(context.Background(), log)

	exitCode := 0
	if err := cmd.ExecuteContext(ctx); err != nil {
		exitCode = 1
	}

	os.Exit(exitCode)
}

// rootCmd constructs the root Cobra command with shared configuration.
func rootCmd() *cobra.Command {
	flag := &rootFlags{}

	cmd := &cobra.Command{
		Use:   appName,
		Short: heredoc.Doc(appShort),

		SilenceErrors: true,
		SilenceUsage:  true,

		ValidArgsFunction: cobra.NoFileCompletions,
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			log := logger.FromContext(cmd.Context())
			log.SetLevel(logger.LevelFromString(flag.logLevel))
		},
	}

	cmd.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		c.PrintErrln(err)
		_ = c.Usage()
		return err
	})

	flag.addFlags(cmd)
	cmd.AddCommand(
		internalcmd.RunCmd(),
		internalcmd.SyncCmd(),
		versionCmd(),
	)

	return cmd
}

// versionCmd constructs the Cobra command that prints version information.
func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   versionCmdName,
		Short: heredoc.Doc(versionShort),

		Args: func(cmd *cobra.Command, args []string) error {
			err := cobra.NoArgs(cmd, args)
			if err != nil {
				cmd.PrintErrln(err)
				_ = cmd.Usage()
			}

			return err
		},
		ValidArgsFunction: cobra.NoFileCompletions,
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintln(cmd.OutOrStdout(), versionString(Version, BuildDate, runtime.Version()))
		},
	}
}

// versionString formats the version metadata for display.
func versionString(version, buildDate, runtimeVersion string) string {
	outputString := version
	if buildDate != "" {
		outputString += " (" + buildDate + ")"
	}

	return outputString + ", Go Version: " + runtimeVersion
}
