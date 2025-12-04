// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package pipeline

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	fakedestination "github.com/mia-platform/ibdm/internal/destination/fake"
	"github.com/mia-platform/ibdm/internal/mapper"
	"github.com/mia-platform/ibdm/internal/source"
	fakesource "github.com/mia-platform/ibdm/internal/source/fake"
)

func TestPipeline(t *testing.T) {
	t.Parallel()

	testMappers := map[string]mapper.Mapper{
		"type1": func() mapper.Mapper {
			mapper, err := mapper.New("{{ .id }}", map[string]string{
				"field1": "{{ .field1 }}",
				"field2": "{{ .field2 }}",
			})
			require.NoError(t, err)
			return mapper
		}(),
		"type2": func() mapper.Mapper {
			mapper, err := mapper.New("{{ .identifier }}", map[string]string{
				"attributeA": "{{ .attributeA }}",
			})
			require.NoError(t, err)
			return mapper
		}(),
	}

	type1 := source.Data{
		Type:      "type1",
		Operation: source.DataOperationUpsert,
		Values: map[string]any{
			"id":     "item1",
			"field1": "value1",
			"field2": "value2",
		},
	}

	type2 := source.Data{
		Type:      "type2",
		Operation: source.DataOperationDelete,
		Values: map[string]any{
			"identifier": "item2",
			"attributeA": "valueA",
		},
	}
	brokenType := source.Data{
		Type:      "type1",
		Operation: source.DataOperationUpsert,
		Values: map[string]any{
			"id":     "item3",
			"field1": "value3",
			// missing field2
		},
	}

	unknownType := source.Data{
		Type:      "unknownType",
		Operation: source.DataOperationUpsert,
		Values: map[string]any{
			"someField": "someValue",
		},
	}

	testCases := map[string]struct {
		source           func(chan<- struct{}) any
		expectedData     []fakedestination.SentDataRecord
		expectedDeletion []string
		expectedErr      error
	}{
		"unsupported source error": {
			source: func(c chan<- struct{}) any {
				c <- struct{}{}
				return "not a valid source"
			},
			expectedErr: errors.ErrUnsupported,
		},
		"source return an error": {
			source: func(c chan<- struct{}) any {
				c <- struct{}{}
				return fakesource.NewFakeEventSourceWithError(t, assert.AnError)
			},
			expectedErr: assert.AnError,
		},
		"valid pipeline return mapped data": {
			source: func(c chan<- struct{}) any {
				return fakesource.NewFakeEventSource(t, []source.Data{type1, brokenType, unknownType, type2}, c)
			},
			expectedData: []fakedestination.SentDataRecord{
				{
					Identifier: "item1",
					Spec: map[string]any{
						"field1": "value1",
						"field2": "value2",
					},
				},
			},
			expectedDeletion: []string{"item2"},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			destination := fakedestination.NewFakeDestination(t)
			syncChan := make(chan struct{}, 1)
			defer close(syncChan)

			testSource := tc.source(syncChan)
			pipeline := New(testSource, testMappers, destination)

			ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
			defer cancel()

			go func() {
				err := pipeline.Start(ctx)
				if tc.expectedErr != nil {
					assert.ErrorIs(t, err, tc.expectedErr)
					syncChan <- struct{}{}
					return
				}

				assert.NoError(t, err)
				syncChan <- struct{}{}
			}()

			<-syncChan
			pipeline.Stop(ctx, 1*time.Second)

			<-syncChan
			assert.Equal(t, tc.expectedData, destination.SentData)
			assert.Equal(t, tc.expectedDeletion, destination.DeletedData)
		})
	}
}

func TestPipelineCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())

	destination := fakedestination.NewFakeDestination(t)
	pipeline := New(fakesource.NewFakeEventSource(t, nil, make(chan<- struct{})), map[string]mapper.Mapper{}, destination)
	cancel()

	err := pipeline.Start(ctx)

	assert.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, destination.SentData)
	assert.Empty(t, destination.DeletedData)
}

func TestClosableSource(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
	defer cancel()

	syncChan := make(chan struct{})
	destination := fakedestination.NewFakeDestination(t)
	pipeline := New(fakesource.NewFakeEventSource(t, []source.Data{}, syncChan), map[string]mapper.Mapper{}, destination)

	defer close(syncChan)
	go func() {
		err := pipeline.Start(ctx)
		assert.NoError(t, err)
	}()

	<-syncChan
	err := pipeline.Stop(ctx, 2*time.Second)
	assert.NoError(t, err)

	assert.Empty(t, destination.SentData)
	assert.Empty(t, destination.DeletedData)
}

func TestNotClosableSourceStop(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())

	destination := fakedestination.NewFakeDestination(t)
	pipeline := New(fakesource.NewHangingEventSource(t), map[string]mapper.Mapper{}, destination)

	go func() {
		err := pipeline.Start(ctx)
		assert.ErrorIs(t, err, context.Canceled)
	}()

	err := pipeline.Stop(ctx, 2*time.Second)
	assert.NoError(t, err)
	cancel()

	assert.Empty(t, destination.SentData)
	assert.Empty(t, destination.DeletedData)
}
