// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package writer

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewWriterDestination(t *testing.T) {
	t.Helper()

	buffer := new(bytes.Buffer)

	destination := NewDestination(buffer)

	destination.SendData(t.Context(), "id-1", map[string]any{
		"key":   "value",
		"array": []string{"a", "b", "c"},
	})

	destination.DeleteData(t.Context(), "id-1")

	expectedOutput := `Send data:
	Identifier: id-1
	Spec:
		{
			"array": [
				"a",
				"b",
				"c"
			],
			"key": "value"
		}

Delete data:
	Identifier: id-1

`

	assert.Equal(t, expectedOutput, buffer.String())
}
