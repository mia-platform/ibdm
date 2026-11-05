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
		"nil":               {err: nil},
		"canceled":          {err: fmt.Errorf("walking: %w", context.Canceled)},
		"deadline exceeded": {err: context.DeadlineExceeded},
		"joined canceled":   {err: errors.Join(errBoom, context.Canceled)},
		"other":             {err: errBoom, expected: errBoom},
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
