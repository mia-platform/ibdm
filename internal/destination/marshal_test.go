// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package destination

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCustomMarshaling(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		input    Data
		expected string
	}{
		"SendData marshals": {
			input: Data{
				APIVersion: "v1",
				ItemFamily: "testResource",
				Name:       "testName",
				Data: map[string]any{
					"key": "value",
				},
			},
			expected: `{"apiVersion":"v1","itemFamily":"testResource","name":"testName","data":{"key":"value"},"operation":"upsert"}`,
		},
		"DeleteData marshals": {
			input: Data{
				APIVersion: "v1",
				ItemFamily: "testResource",
				Name:       "testName",
				Data:       nil,
			},
			expected: `{"apiVersion":"v1","itemFamily":"testResource","name":"testName","operation":"delete"}`,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			marshaled, err := json.Marshal(tc.input)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, string(marshaled))
		})
	}
}
