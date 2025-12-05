// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package cmd

import (
	"errors"
	"strings"

	"github.com/spf13/cobra"
)

var (
	errNoArguments        = errors.New("no integration name provided")
	errInvalidIntegration = errors.New("invalid integration name provided")

	// availableEventSources holds the list of available integration sources and their description
	// for command completion and help messages.
	availableEventSources = map[string]string{
		"gcp": "Google Cloud Platform integration",
	}
	// availableSyncSources holds the list of available synchronization sources and their description
	// for command completion and help messages.
	availableSyncSources = map[string]string{
		"gcp": "Google Cloud Platform synchronization",
	}
)

// handleError will do custom print error handling based on the type of error received.
// it will return nil if the command must return 0 exit code, otherwise it will return
// the original error.
func handleError(cmd *cobra.Command, err error) error {
	switch {
	case errors.Is(err, errNoArguments):
		_ = cmd.Usage() // do not check error as we cannot do much about it
		return nil
	case errors.Is(err, errInvalidIntegration):
		cmd.PrintErrln(err)
		_ = cmd.Usage() // do not check error as we cannot do much about it
		return err
	default:
		cmd.PrintErrln(err)
		return err
	}
}

// unwrappedError returns the unwrapped error if available, otherwise it returns the original error.
func unwrappedError(err error) error {
	if unwrapped := errors.Unwrap(err); unwrapped != nil {
		return unwrapped
	}

	return err
}

func validArgsFunc(sources map[string]string) cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		var comps []string
		if len(args) == 0 {
			for name, description := range sources {
				if strings.HasPrefix(name, toComplete) {
					comps = append(comps, cobra.CompletionWithDesc(name, description))
				}
			}
		}

		return comps, cobra.ShellCompDirectiveNoFileComp
	}
}
