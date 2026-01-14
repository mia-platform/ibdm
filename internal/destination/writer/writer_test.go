// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package writer

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mia-platform/ibdm/internal/destination"
)

func TestNewWriterDestination(t *testing.T) {
	t.Helper()

	buffer := new(bytes.Buffer)

	testDestination := NewDestination(buffer)

	testDestination.SendData(t.Context(), &destination.Data{
		APIVersion: "v1",
		Resource:   "resources",
		Name:       "id-1",
		Data: map[string]any{
			"key":   "value",
			"array": []string{"a", "b", "c"},
		},
		OperationTime: "2020-01-01T00:00:00Z",
	})

	testDestination.DeleteData(t.Context(), &destination.Data{
		APIVersion:    "v1",
		Resource:      "resources",
		Name:          "id-1",
		OperationTime: "2020-01-01T00:00:00Z",
	})

	expectedOutput := `Send data:
	APIVersion: v1
	Resource: resources
	Resource Name: id-1
	Timestamp: 2020-01-01T00:00:00Z
	Spec: {
		"array": [
			"a",
			"b",
			"c"
		],
		"key": "value"
	}

Delete data:
	APIVersion: v1
	Resource: resources
	Resource Name: id-1
	Timestamp: 2020-01-01T00:00:00Z

`

	assert.Equal(t, expectedOutput, buffer.String())
}
