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

	"github.com/mia-platform/ibdm/internal/destination"
	fakedestination "github.com/mia-platform/ibdm/internal/destination/fake"
	"github.com/mia-platform/ibdm/internal/mapper"
	"github.com/mia-platform/ibdm/internal/source"
	fakesource "github.com/mia-platform/ibdm/internal/source/fake"
)

var (
	testTime = time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	type1    = source.Data{
		Type:      "type1",
		Operation: source.DataOperationUpsert,
		Values: map[string]any{
			"id":     "item1",
			"field1": "value1",
			"field2": "value2",
		},
		Time: testTime,
	}

	type2 = source.Data{
		Type:      "type2",
		Operation: source.DataOperationDelete,
		Values: map[string]any{
			"identifier": "item2",
		},
		Time: testTime,
	}
	brokenType = source.Data{
		Type:      "type1",
		Operation: source.DataOperationUpsert,
		Values: map[string]any{
			"id":     "item3",
			"field1": "value3",
			// missing field2
		},
		Time: testTime,
	}

	unknownType = source.Data{
		Type:      "unknownType",
		Operation: source.DataOperationUpsert,
		Values: map[string]any{
			"someField": "someValue",
		},
		Time: testTime,
	}
)

func testMappers(tb testing.TB, extra []map[string]any) map[string]DataMapper {
	tb.Helper()

	return map[string]DataMapper{
		"type1": func() DataMapper {
			mapper, err := mapper.New("{{ .id }}", map[string]string{
				"field1": "{{ .field1 }}",
				"field2": "{{ .field2 }}",
			}, extra)
			require.NoError(tb, err)
			return DataMapper{
				APIVersion: "v1",
				Resource:   "resource",
				Mapper:     mapper,
			}
		}(),
		"type2": func() DataMapper {
			mapper, err := mapper.New("{{ .identifier }}", map[string]string{
				"attributeA": "{{ .attributeA }}",
			}, extra)
			require.NoError(tb, err)
			return DataMapper{
				APIVersion: "v2",
				Resource:   "resource2",
				Mapper:     mapper,
			}
		}(),
	}
}

func getMappingsExtra(tb testing.TB, returnExtra bool) []map[string]any {
	tb.Helper()

	if !returnExtra {
		return nil
	}

	extraDef := map[string]any{
		"apiVersion": "relationships/v1",
		"resource":   "relationships",
		"identifier": `{{ printf "relationship--%s--%s--dependency" .field1 .field2 }}`,
		"sourceRef": map[string]any{
			"apiVersion": "resource.custom-platform/v1",
			"kind":       "resource1",
			"name":       "{{ .field2 }}",
		},
		"type": "dependency",
	}

	return []map[string]any{extraDef}
}

func TestStreamPipeline(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		source           func(chan<- struct{}) any
		expectedData     []*destination.Data
		expectedDeletion []*destination.Data
		expectedErr      error
		useExtra         bool
	}{
		"unsupported source error": {
			source: func(c chan<- struct{}) any {
				c <- struct{}{}
				return "not a valid source"
			},
			expectedErr: errors.ErrUnsupported,
			useExtra:    false,
		},
		"source return an error": {
			source: func(c chan<- struct{}) any {
				c <- struct{}{}
				return fakesource.NewFakeSourceWithError(t, assert.AnError)
			},
			expectedErr: assert.AnError,
			useExtra:    false,
		},
		"valid pipeline return mapped data": {
			source: func(c chan<- struct{}) any {
				return fakesource.NewFakeEventSource(t, []source.Data{type1, brokenType, unknownType, type2}, c)
			},
			expectedData: []*destination.Data{
				{
					APIVersion: "v1",
					Resource:   "resource",
					Name:       "item1",
					Data: map[string]any{
						"field1": "value1",
						"field2": "value2",
					},
					OperationTime: "2024-06-01T12:00:00Z",
				},
			},
			expectedDeletion: []*destination.Data{
				{
					APIVersion:    "v2",
					Resource:      "resource2",
					Name:          "item2",
					OperationTime: "2024-06-01T12:00:00Z",
				},
			},
			useExtra: false,
		},
		"valid pipeline return mapped data with extra mappings": {
			source: func(c chan<- struct{}) any {
				return fakesource.NewFakeEventSource(t, []source.Data{type1, brokenType, unknownType, type2}, c)
			},
			expectedData: []*destination.Data{
				{
					APIVersion: "v1",
					Resource:   "resource",
					Name:       "item1",
					Data: map[string]any{
						"field1": "value1",
						"field2": "value2",
					},
					OperationTime: "2024-06-01T12:00:00Z",
				},
				{
					APIVersion: "relationships/v1",
					Resource:   "relationships",
					Name:       "relationship--value1--value2--dependency",
					Data: map[string]any{
						"sourceRef": map[string]any{
							"apiVersion": "resource.custom-platform/v1",
							"kind":       "resource1",
							"name":       "value2",
						},
						"targetRef": map[string]any{
							"apiVersion": "v1",
							"kind":       "resource",
							"name":       "item1",
						},
						"type": "dependency",
					},
					OperationTime: "2024-06-01T12:00:00Z",
				},
			},
			expectedDeletion: []*destination.Data{
				{
					APIVersion:    "v2",
					Resource:      "resource2",
					Name:          "item2",
					OperationTime: "2024-06-01T12:00:00Z",
				},
			},
			useExtra: true,
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()

			destination := fakedestination.NewFakeDestination(t)
			syncChan := make(chan struct{}, 1)
			defer close(syncChan)

			testSource := test.source(syncChan)
			extra := getMappingsExtra(t, test.useExtra)
			pipeline, err := New(ctx, testSource, testMappers(t, extra), destination)
			require.NoError(t, err)

			ctx, cancel := context.WithTimeout(ctx, 1*time.Second)
			defer cancel()

			go func() {
				err := pipeline.Start(ctx)
				if test.expectedErr != nil {
					assert.ErrorIs(t, err, test.expectedErr)
					syncChan <- struct{}{}
					return
				}

				assert.NoError(t, err)
				syncChan <- struct{}{}
			}()

			<-syncChan
			pipeline.Stop(ctx, 1*time.Second)

			<-syncChan
			assert.Equal(t, test.expectedData, destination.SentData)
			assert.Equal(t, test.expectedDeletion, destination.DeletedData)
		})
	}
}

func TestStreamPipelineWebhook(t *testing.T) {
	t.Setenv("HTTP_PORT", "0")

	testCases := map[string]struct {
		expectedData []*destination.Data
		expectedErr  error
		useExtra     bool
	}{
		"valid webhook pipeline return mapped data without extra mappings": {
			expectedData: []*destination.Data{
				{
					APIVersion: "v1",
					Resource:   "resource",
					Name:       "item1",
					Data: map[string]any{
						"field1": "value1",
						"field2": "value2",
					},
					OperationTime: "2024-06-01T12:00:00Z",
				},
			},
		},
		"valid webhook pipeline return mapped data with extra mappings": {
			expectedData: []*destination.Data{
				{
					APIVersion: "v1",
					Resource:   "resource",
					Name:       "item1",
					Data: map[string]any{
						"field1": "value1",
						"field2": "value2",
					},
					OperationTime: "2024-06-01T12:00:00Z",
				},
				{
					APIVersion: "relationships/v1",
					Resource:   "relationships",
					Name:       "relationship--value1--value2--dependency",
					Data: map[string]any{
						"sourceRef": map[string]any{
							"apiVersion": "resource.custom-platform/v1",
							"kind":       "resource1",
							"name":       "value2",
						},
						"targetRef": map[string]any{
							"apiVersion": "v1",
							"kind":       "resource",
							"name":       "item1",
						},
						"type": "dependency",
					},
					OperationTime: "2024-06-01T12:00:00Z",
				},
			},
			useExtra: true,
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()

			destination := fakedestination.NewFakeDestination(t)
			syncChan := make(chan struct{})

			source := fakesource.NewFakeUnclosableWebhookSource(t, []source.Data{type1, brokenType, unknownType, type2}, syncChan)
			extra := getMappingsExtra(t, test.useExtra)
			pipeline, err := New(ctx, source, testMappers(t, extra), destination)
			require.NoError(t, err)

			go func() {
				defer close(syncChan)
				err := pipeline.Start(ctx)
				if test.expectedErr != nil {
					assert.ErrorIs(t, err, test.expectedErr)
					return
				}
			}()

			source.SimulateWebhook()
			<-syncChan
			assert.NoError(t, pipeline.server.Stop())
			select {
			case <-syncChan:
			case <-ctx.Done():
				require.Fail(t, "timeout waiting for pipeline to stop")
			}

			assert.Equal(t, test.expectedData, destination.SentData)
			// assert.Equal(t, test.expectedDeletion, destination.DeletedData)
		})
	}
}

func TestStreamPipelineCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())

	destination := fakedestination.NewFakeDestination(t)
	pipeline, err := New(ctx, fakesource.NewFakeEventSource(t, nil, make(chan<- struct{})), map[string]DataMapper{}, destination)
	require.NoError(t, err)
	cancel()

	err = pipeline.Start(ctx)

	assert.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, destination.SentData)
	assert.Empty(t, destination.DeletedData)
}

func TestStreamClosableSource(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
	defer cancel()

	syncChan := make(chan struct{})

	destination := fakedestination.NewFakeDestination(t)
	pipeline, err := New(ctx, fakesource.NewFakeEventSource(t, []source.Data{}, syncChan), map[string]DataMapper{}, destination)
	require.NoError(t, err)
	go func() {
		err := pipeline.Start(ctx)
		assert.NoError(t, err)
		close(syncChan)
	}()

	<-syncChan
	err = pipeline.Stop(ctx, 2*time.Second)
	assert.NoError(t, err)

	<-syncChan
	assert.Empty(t, destination.SentData)
	assert.Empty(t, destination.DeletedData)
}

func TestNotClosableSourceStop(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())

	destination := fakedestination.NewFakeDestination(t)

	syncChan := make(chan struct{})
	pipeline, err := New(ctx, fakesource.NewFakeUnclosableEventSource(t, nil, syncChan), map[string]DataMapper{}, destination)
	require.NoError(t, err)
	go func() {
		err := pipeline.Start(ctx)
		assert.ErrorIs(t, err, context.Canceled)
		close(syncChan)
	}()

	err = pipeline.Stop(ctx, 2*time.Second)
	assert.NoError(t, err)
	cancel()

	<-syncChan
	assert.Empty(t, destination.SentData)
	assert.Empty(t, destination.DeletedData)
}

func TestSyncPipeline(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		source           any
		expectedData     []*destination.Data
		expectedDeletion []*destination.Data
		expectedErr      error
	}{
		"unsupported source error": {
			source:      "not a valid source",
			expectedErr: errors.ErrUnsupported,
		},
		"source return an error": {
			source:      fakesource.NewFakeSourceWithError(t, assert.AnError),
			expectedErr: assert.AnError,
		},
		"valid pipeline return mapped data": {
			source: fakesource.NewFakeSyncableSource(t, []source.Data{type1, brokenType, unknownType, type2}),
			expectedData: []*destination.Data{
				{
					APIVersion: "v1",
					Resource:   "resource",
					Name:       "item1",
					Data: map[string]any{
						"field1": "value1",
						"field2": "value2",
					},
					OperationTime: "2024-06-01T12:00:00Z",
				},
			},
			expectedDeletion: []*destination.Data{
				{
					APIVersion:    "v2",
					Resource:      "resource2",
					Name:          "item2",
					OperationTime: "2024-06-01T12:00:00Z",
				},
			},
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
			destination := fakedestination.NewFakeDestination(t)
			pipeline, err := New(ctx, test.source, testMappers(t, nil), destination)
			require.NoError(t, err)

			defer cancel()

			err = pipeline.Sync(ctx)
			if test.expectedErr != nil {
				assert.ErrorIs(t, err, test.expectedErr)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, test.expectedData, destination.SentData)
			assert.Equal(t, test.expectedDeletion, destination.DeletedData)
		})
	}
}

func TestSyncPipelineCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())

	destination := fakedestination.NewFakeDestination(t)
	pipeline, err := New(ctx, fakesource.NewFakeSyncableSource(t, nil), map[string]DataMapper{}, destination)
	require.NoError(t, err)
	cancel()

	err = pipeline.Sync(ctx)

	assert.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, destination.SentData)
	assert.Empty(t, destination.DeletedData)
}

func TestSyncClosableSource(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
	defer cancel()

	destination := fakedestination.NewFakeDestination(t)
	pipeline, err := New(ctx, fakesource.NewFakeSyncableSource(t, []source.Data{}), map[string]DataMapper{}, destination)
	require.NoError(t, err)
	assert.NoError(t, pipeline.Stop(ctx, 2*time.Second))
	assert.NoError(t, pipeline.Sync(ctx))

	assert.Empty(t, destination.SentData)
	assert.Empty(t, destination.DeletedData)
}
