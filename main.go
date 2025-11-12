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
	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var (
	// Version is dynamically set by the ci or overridden by the Makefile.
	Version = "DEV"
	// BuildDate is dynamically set at build time by the cli or overridden in the Makefile.
	BuildDate = "" // YYYY-MM-DD
)

const (
	appName  = "ibdm"
	appShort = "ibdm is the CLI tool to create a Mia-Platform Catalog connector"

	logLevelFlagName       = "log-level"
	logeLevelShortFlagName = "v"

	versionCmdName = "version"
	versionShort   = "Display the " + appName + " version."
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

type rootFlags struct {
	logLevel string
}

func (f *rootFlags) AddFlags(flags *pflag.FlagSet) {
	flags.StringVarP(&f.logLevel, logLevelFlagName, logeLevelShortFlagName, logLevelDefaultValue, heredoc.Doc(logLevelFlagUsage))
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

// rootCmd return the base cobra command correctly configured
func rootCmd() *cobra.Command {
	flag := &rootFlags{}

	cmd := &cobra.Command{
		Use:   appName,
		Short: heredoc.Doc(appShort),

		SilenceErrors: true,
		SilenceUsage:  true,

		Args: cobra.NoArgs,

		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			log := logger.FromContext(cmd.Context())
			log.SetLevel(logger.LevelFromString(flag.logLevel))
		},
	}

	flag.AddFlags(cmd.PersistentFlags())
	cmd.AddCommand(versionCmd())

	return cmd
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   versionCmdName,
		Short: heredoc.Doc(versionShort),

		Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintln(cmd.OutOrStdout(), versionString(Version, BuildDate, runtime.Version()))
		},
	}
}

func versionString(version, buildDate, runtimeVersion string) string {
	outputString := version
	if buildDate != "" {
		outputString += " (" + buildDate + ")"
	}

	return outputString + ", Go Version: " + runtimeVersion
}
