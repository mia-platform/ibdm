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
	})

	testDestination.DeleteData(t.Context(), &destination.Data{
		APIVersion: "v1",
		Resource:   "resources",
		Name:       "id-1",
	})

	expectedOutput := `Send data:
	APIVersion: v1
	Resource: resources
	Resource Name: id-1
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

`

	assert.Equal(t, expectedOutput, buffer.String())
}
