// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package mapper

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMapper(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		mapper        Mapper
		input         map[string]any
		expected      map[string]any
		expectedError error
	}{}

	for testName, test := range testCases {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			output, err := test.mapper.ApplyTemplates(test.input)
			if test.expectedError != nil {
				assert.Nil(t, output)
				assert.ErrorIs(t, err, test.expectedError)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, test.expected, output)
		})
	}
}
