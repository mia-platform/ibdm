// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/caarlos0/env/v11"
	"github.com/stretchr/testify/require"
)

func TestConsoleError(t *testing.T) {
	t.Parallel()

	t.Run("Error returns prefixed message", func(t *testing.T) {
		t.Parallel()

		err := &ConsoleError{err: errors.New("something went wrong")}
		require.Equal(t, "console: something went wrong", err.Error())
	})

	t.Run("Unwrap returns underlying error", func(t *testing.T) {
		t.Parallel()

		originalErr := errors.New("root cause")
		err := &ConsoleError{err: originalErr}
		require.Equal(t, originalErr, errors.Unwrap(err))
	})

	t.Run("Is matches similar ConsoleError", func(t *testing.T) {
		t.Parallel()

		err1 := &ConsoleError{err: errors.New("foo")}
		err2 := &ConsoleError{err: errors.New("foo")}
		err3 := &ConsoleError{err: errors.New("bar")}
		otherErr := errors.New("foo")

		require.ErrorIs(t, err1, err2)
		require.NotErrorIs(t, err1, err3)
		require.NotErrorIs(t, err1, otherErr)
	})
}

func TestHandleError(t *testing.T) {
	t.Parallel()

	t.Run("wraps normal error", func(t *testing.T) {
		t.Parallel()

		err := errors.New("boom")
		result := handleError(err)

		var ce *ConsoleError
		require.ErrorAs(t, result, &ce)
		require.Equal(t, "console: boom", result.Error())
	})

	t.Run("unwraps env.AggregateError", func(t *testing.T) {
		t.Parallel()

		// Create an env.AggregateError
		envErr := env.AggregateError{
			Errors: []error{
				errors.New("env error 1"),
				errors.New("env error 2"),
			},
		}

		result := handleError(envErr)

		var ce *ConsoleError
		require.ErrorAs(t, result, &ce)
		// Should take the first error from aggregate and wrap it
		require.Equal(t, "console: env error 1", result.Error())
	})

	t.Run("returns nil on context canceled", func(t *testing.T) {
		t.Parallel()

		result := handleError(context.Canceled)
		require.NoError(t, result)
	})

	t.Run("returns nil when context canceled is wrapped", func(t *testing.T) {
		t.Parallel()

		result := handleError(fmt.Errorf("wrapped: %w", context.Canceled))
		require.NoError(t, result)
	})
}
