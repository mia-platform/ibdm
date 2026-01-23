// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package service

import (
	"context"
	"errors"

	"github.com/caarlos0/env/v11"
)

// ConsoleError wraps lower-level errors produced by the Console.
type ConsoleError struct {
	err error
}

func (e *ConsoleError) Error() string {
	return "console: " + e.err.Error()
}

func (e *ConsoleError) Unwrap() error {
	return e.err
}

func (e *ConsoleError) Is(target error) bool {
	cre, ok := target.(*ConsoleError)
	if !ok {
		return false
	}

	return e.err.Error() == cre.err.Error()
}

// handleError normalizes errors emitted by the Console Service.
func handleError(err error) error {
	var parseErr env.AggregateError
	if errors.As(err, &parseErr) {
		err = parseErr.Errors[0]
	}

	if errors.Is(err, context.Canceled) {
		return nil
	}

	return &ConsoleError{
		err: err,
	}
}
