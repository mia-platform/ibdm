// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package fake

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mia-platform/ibdm/internal/destination"
)

func TestFakeDestination(t *testing.T) {
	t.Parallel()

	fakeDestination := NewFakeDestination(t)
	assert.Empty(t, fakeDestination.SentData)
	assert.Empty(t, fakeDestination.DeletedData)

	data := &destination.Data{
		APIVersion: "version",
		Resource:   "resource",
		Name:       "id1",
		Data:       map[string]any{"key": "value"},
	}

	assert.NoError(t, fakeDestination.SendData(t.Context(), data))
	assert.Equal(t, []*destination.Data{data}, fakeDestination.SentData)

	deleteData := &destination.Data{
		APIVersion: "version",
		Resource:   "resource",
		Name:       "id1",
	}
	assert.NoError(t, fakeDestination.DeleteData(t.Context(), deleteData))
	assert.Equal(t, []*destination.Data{deleteData}, fakeDestination.DeletedData)
}
