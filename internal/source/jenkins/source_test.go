// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package jenkins

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleErr(t *testing.T) {
	t.Parallel()

	errBoom := errors.New("boom")

	testCases := map[string]struct {
		err      error
		expected error
	}{
		"nil":                       {err: nil},
		"canceled":                  {err: fmt.Errorf("walking: %w", context.Canceled)},
		"joined cancellations":      {err: errors.Join(context.Canceled, fmt.Errorf("sending: %w", context.Canceled))},
		"deadline exceeded":         {err: context.DeadlineExceeded, expected: context.DeadlineExceeded},
		"joined failure and cancel": {err: errors.Join(errBoom, context.Canceled), expected: errBoom},
		"wrapped joined failure":    {err: fmt.Errorf("instance: %w", errors.Join(errBoom, context.Canceled)), expected: errBoom},
		"other":                     {err: errBoom, expected: errBoom},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := handleErr(tc.err)
			if tc.expected == nil {
				assert.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, ErrJenkinsSource)
			assert.ErrorIs(t, err, tc.expected)
		})
	}
}
