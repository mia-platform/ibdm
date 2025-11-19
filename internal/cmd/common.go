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
)

// handleError will do custom print error handling based on the type of error received.
// it will return nil if the command must return 0 exit code, otherwise it will return
// the original error.
func handleError(cmd *cobra.Command, err error) error {
	switch {
	case errors.Is(err, errNoArguments):
		_ = cmd.Usage() // do not check error as we cannot do much about it
		return nil
	default:
		cmd.PrintErrln(err)
		_ = cmd.Usage() // do not check error as we cannot do much about it
		return err
	}
}
