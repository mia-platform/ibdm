// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package source

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDataOperationString(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		input    DataOperation
		expected string
	}{
		"DataOperationUpsert": {
			input:    DataOperationUpsert,
			expected: "Upsert",
		},
		"DataOperationDelete": {
			input:    DataOperationDelete,
			expected: "Delete",
		},
		"Unknown value": {
			input:    DataOperation(999),
			expected: "DataOperation(999)",
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, test.expected, test.input.String())
		})
	}
}
