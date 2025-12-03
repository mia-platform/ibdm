// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package cmd

import (
	"errors"

	"github.com/spf13/cobra"
)

var (
	errNoArguments        = errors.New("no integration name provided")
	errInvalidIntegration = errors.New("invalid integration name provided")

	// availableSources holds the list of available integration sources and their description
	// for command completion and help messages.
	availableSources = map[string]string{
		"gcp": "Google Cloud Platform integration",
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
