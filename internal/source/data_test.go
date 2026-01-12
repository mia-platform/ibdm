// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package source

import (
	"testing"
	"time"

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

func TestDataTimestamp(t *testing.T) {
	t.Parallel()
	nowFunc = func() time.Time {
		return time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	}

	testCases := map[string]struct {
		data     Data
		expected string
	}{
		"With explicit time": {
			data: Data{
				Time: time.Date(2023, 5, 15, 10, 30, 0, 0, time.UTC),
			},
			expected: "2023-05-15T10:30:00Z",
		},
		"With zero time": {
			data:     Data{},
			expected: "2024-06-01T12:00:00Z",
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, test.expected, test.data.Timestamp())
		})
	}
}
