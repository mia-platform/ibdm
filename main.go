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
	// Version is dynamically set by the ci or overridden by the Makefile.
	Version = info.Version
	// BuildDate is dynamically set at build time by the cli or overridden in the Makefile.
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

// rootFlags holds the global flags for the root command.
type rootFlags struct {
	logLevel string
}

// addFlags adds the cli flags to the cobra command.
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

// rootCmd return the base cobra command correctly configured.
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

// versionCmd returns the cobra command that prints the version information.
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

// versionString formats the version information string.
func versionString(version, buildDate, runtimeVersion string) string {
	outputString := version
	if buildDate != "" {
		outputString += " (" + buildDate + ")"
	}

	return outputString + ", Go Version: " + runtimeVersion
}
