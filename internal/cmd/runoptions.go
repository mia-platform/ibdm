// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// runFlags holds the flags for the "run" command.
type runFlags struct{}

// addFlags adds the cli flags to the cobra command.
func (f *runFlags) addFlags(_ *cobra.Command) {
}

// toOptions converts the run flags to runOptions enriching it with the passed arguments.
func (f *runFlags) toOptions(args []string) *runOptions {
	integrationName := ""
	if len(args) > 0 {
		integrationName = args[0]
	}

	return &runOptions{
		integrationName: integrationName,
	}
}

// runOptions holds the options set for the current run function.
type runOptions struct {
	integrationName string
}

// validate validates the run options and returns an error if something is wrong.
func (o *runOptions) validate() error {
	if o.integrationName == "" {
		return errNoArguments
	}

	validIntegrations := map[string]bool{
		"gcp": true,
	}
	if !validIntegrations[o.integrationName] {
		return fmt.Errorf("%w: %s", errInvalidIntegration, o.integrationName)
	}

	return nil
}
