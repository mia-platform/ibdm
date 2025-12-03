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

func TestFakeEventSource(t *testing.T) {
	t.Parallel()
	testTimout := 1 * time.Second
	testData := []source.Data{
		{Type: "1", Operation: source.DataOperationUpsert, Values: map[string]any{"key": "value"}},
		{Type: "2", Operation: source.DataOperationDelete, Values: map[string]any{"key": "value"}},
		{Type: "3", Operation: source.DataOperationUpsert, Values: map[string]any{"key": "value"}},
	}
	ctx, cancel := context.WithTimeout(t.Context(), testTimout)
	defer cancel()

	syncTestChan := make(chan struct{})
	defer close(syncTestChan)

	receiveDataChan := make(chan source.Data)
	defer close(receiveDataChan)

	fakeSource := NewFakeEventSource(t, testData, syncTestChan)
	go func() {
		err := fakeSource.StartEventStream(ctx, nil, receiveDataChan)
		assert.NoError(t, err)
		syncTestChan <- struct{}{}
	}()

	receivedData := make([]source.Data, 0)
loop:
	for {
		select {
		case <-syncTestChan:
			break loop
		case <-ctx.Done():
			assert.Fail(t, "context cancelled", "error", ctx.Err())
			return
		case data := <-receiveDataChan:
			receivedData = append(receivedData, data)
		}
	}

	fakeSource.Close(ctx, testTimout)
	<-syncTestChan
	assert.Equal(t, receivedData, testData)
}

func TestFakeEventSourceWithError(t *testing.T) {
	t.Parallel()
	testTimout := 1 * time.Second
	testError := assert.AnError

	ctx, cancel := context.WithTimeout(t.Context(), testTimout)
	defer cancel()

	errorSource := NewFakeEventSourceWithError(t, testError)
	receiveDataChan := make(chan source.Data)
	defer close(receiveDataChan)

	err := errorSource.StartEventStream(ctx, nil, receiveDataChan)
	assert.Equal(t, testError, err)
}

func TestFakeEventSourceCancelledContext(t *testing.T) {
	t.Parallel()
	testData := []source.Data{
		{Type: "1", Operation: source.DataOperationUpsert, Values: map[string]any{"key": "value"}},
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	receiveDataChan := make(chan source.Data)
	defer close(receiveDataChan)

	fakeSource := NewFakeEventSource(t, testData, make(chan struct{}))
	err := fakeSource.StartEventStream(ctx, nil, receiveDataChan)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, receiveDataChan)
}
