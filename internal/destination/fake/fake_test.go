// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package fake

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFakeDestination(t *testing.T) {
	t.Parallel()

	destination := NewFakeDestination(t)
	assert.Empty(t, destination.SentData)
	assert.Empty(t, destination.DeletedData)

	data := SentDataRecord{
		Identifier: "id1",
		Spec:       map[string]any{"key": "value"},
	}

	assert.NoError(t, destination.SendData(t.Context(), data.Identifier, data.Spec))
	assert.Equal(t, []SentDataRecord{data}, destination.SentData)

	assert.NoError(t, destination.DeleteData(t.Context(), data.Identifier))
	assert.Equal(t, []string{data.Identifier}, destination.DeletedData)
}
