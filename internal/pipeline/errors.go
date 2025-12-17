// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package pipeline

import "errors"

// unsupportedSourceError signals that the configured source does not implement the required capability.
type unsupportedSourceError struct {
	Message string
}

func (e *unsupportedSourceError) Error() string {
	return e.Message
}

func (e *unsupportedSourceError) Unwrap() error {
	return errors.ErrUnsupported
}
