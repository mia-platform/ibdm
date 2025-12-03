// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package fake

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/mia-platform/ibdm/internal/source"
)

func TestFakeSyncableSource(t *testing.T) {
	t.Parallel()
	testTimeout := 1 * time.Second
	testData := []source.Data{
		{Type: "1", Operation: source.DataOperationUpsert, Values: map[string]any{"key": "value"}},
		{Type: "2", Operation: source.DataOperationDelete, Values: map[string]any{"key": "value"}},
		{Type: "3", Operation: source.DataOperationUpsert, Values: map[string]any{"key": "value"}},
	}

	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	defer cancel()

	receiveDataChan := make(chan source.Data)

	fakeSource := NewFakeSyncableSource(t, testData)
	go func() {
		err := fakeSource.StartSyncProcess(ctx, nil, receiveDataChan)
		close(receiveDataChan)
		assert.NoError(t, err)
	}()

	receivedData := make([]source.Data, 0)
	readMessages := 0
	for {
		if len(receivedData) == 1 {
			fakeSource.Close(ctx, testTimeout)
		}

		data, ok := <-receiveDataChan
		if !ok {
			break
		}
		receivedData = append(receivedData, data)
		readMessages++
	}

	assert.Equal(t, testData[:readMessages], receivedData)
}

func TestFakeSyncableSourceCancelledContext(t *testing.T) {
	t.Parallel()
	testData := []source.Data{
		{Type: "1", Operation: source.DataOperationUpsert, Values: map[string]any{"key": "value"}},
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	receiveDataChan := make(chan source.Data)
	defer close(receiveDataChan)

	fakeSource := NewFakeSyncableSource(t, testData)
	err := fakeSource.StartSyncProcess(ctx, nil, receiveDataChan)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, receiveDataChan)
}
