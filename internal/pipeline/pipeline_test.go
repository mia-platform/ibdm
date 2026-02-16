// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package pipeline

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/config"
	"github.com/mia-platform/ibdm/internal/destination"
	fakedestination "github.com/mia-platform/ibdm/internal/destination/fake"
	"github.com/mia-platform/ibdm/internal/mapper"
	"github.com/mia-platform/ibdm/internal/server"
	fakeserver "github.com/mia-platform/ibdm/internal/server/fake"
	"github.com/mia-platform/ibdm/internal/source"
	fakesource "github.com/mia-platform/ibdm/internal/source/fake"
)

var (
	testTime = time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)

	type1 = source.Data{
		Type:      "type1",
		Operation: source.DataOperationUpsert,
		Values: map[string]any{
			"id":     "item1",
			"field1": "value1",
			"field2": "value2",
		},
		Time: testTime,
	}

	type1D = source.Data{
		Type:      "type1",
		Operation: source.DataOperationDelete,
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

	deleteExtra = source.Data{
		Type:      "relationships",
		Operation: source.DataOperationDelete,
		Values: map[string]any{
			"identifier": "relationship--value1--value2--dependency",
		},
		Time: testTime,
	}
)

func testMappers(tb testing.TB, extra []config.Extra) map[string]DataMapper {
	tb.Helper()

	return map[string]DataMapper{
		"type1": func() DataMapper {
			mapper, err := mapper.New("{{ .id }}", nil, map[string]string{
				"field1": "{{ .field1 }}",
				"field2": "{{ .field2 }}",
			}, extra)
			require.NoError(tb, err)
			return DataMapper{
				APIVersion: "v1",
				ItemFamily: "family",
				Mapper:     mapper,
			}
		}(),
		"type2": func() DataMapper {
			mapper, err := mapper.New("{{ .identifier }}", nil, map[string]string{
				"attributeA": "{{ .attributeA }}",
			}, extra)
			require.NoError(tb, err)
			return DataMapper{
				APIVersion: "v2",
				ItemFamily: "family2",
				Mapper:     mapper,
			}
		}(),
	}
}

func getExtra(tb testing.TB, deletePolicy string, id int) config.Extra {
	tb.Helper()

	idString := ""
	if id > 0 {
		idString = "-" + strconv.Itoa(id)
	}
	return config.Extra{
		"apiVersion":   "relationships/v1",
		"itemFamily":   "relationships",
		"deletePolicy": deletePolicy,
		"identifier":   `{{ printf "relationship--%s--%s--dependency` + idString + `" .field1 .field2 }}`,
		"sourceRef": map[string]any{
			"apiVersion": "resource.custom-platform/v1",
			"family":     "family1",
			"name":       "{{ .field2 }}" + idString,
		},
		"type": "dependency",
	}
}

func getMappingsExtra(tb testing.TB, returnExtra bool, deletePolicy string, extrasLen int) []config.Extra {
	tb.Helper()

	if !returnExtra {
		return nil
	}

	if deletePolicy == "" && deletePolicy != "none" && deletePolicy != "cascade" {
		deletePolicy = "none"
	}

	extras := make([]config.Extra, extrasLen)
	for i := range extrasLen {
		extras[i] = getExtra(tb, deletePolicy, i)
	}

	return extras
}

func TestStreamPipeline(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		source           func(chan<- struct{}) any
		expectedData     []*destination.Data
		expectedDeletion []*destination.Data
		expectedErr      error
		useExtra         bool
		numExtras        int
		deletePolicy     string
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
					ItemFamily: "family",
					Name:       "item1",
					Metadata:   map[string]any{},
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
					ItemFamily:    "family2",
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
					ItemFamily: "family",
					Name:       "item1",
					Metadata:   map[string]any{},
					Data: map[string]any{
						"field1": "value1",
						"field2": "value2",
					},
					OperationTime: "2024-06-01T12:00:00Z",
				},
				{
					APIVersion: "relationships/v1",
					ItemFamily: "relationships",
					Name:       "relationship--value1--value2--dependency",
					Data: map[string]any{
						"sourceRef": map[string]any{
							"apiVersion": "resource.custom-platform/v1",
							"family":     "family1",
							"name":       "value2",
						},
						"targetRef": map[string]any{
							"apiVersion": "v1",
							"family":     "family",
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
					ItemFamily:    "family2",
					Name:          "item2",
					OperationTime: "2024-06-01T12:00:00Z",
				},
			},
			useExtra: true,
		},
		"valid pipeline return deletion with extra mappings delete cascade": {
			source: func(c chan<- struct{}) any {
				return fakesource.NewFakeEventSource(t, []source.Data{type1D, deleteExtra}, c)
			},
			expectedData: nil,
			expectedDeletion: []*destination.Data{
				{
					APIVersion:    "v1",
					ItemFamily:    "family",
					Name:          "item1",
					OperationTime: "2024-06-01T12:00:00Z",
				},
				{
					APIVersion:    "relationships/v1",
					ItemFamily:    "relationships",
					Name:          "relationship--value1--value2--dependency",
					OperationTime: "2024-06-01T12:00:00Z",
				},
			},
			useExtra:     true,
			deletePolicy: "cascade",
		},
		"valid pipeline return mapped data with multiple extra mappings": {
			source: func(c chan<- struct{}) any {
				return fakesource.NewFakeEventSource(t, []source.Data{type1}, c)
			},
			expectedData: []*destination.Data{
				{
					APIVersion: "v1",
					ItemFamily: "family",
					Name:       "item1",
					Metadata:   map[string]any{},
					Data: map[string]any{
						"field1": "value1",
						"field2": "value2",
					},
					OperationTime: "2024-06-01T12:00:00Z",
				},
				{
					APIVersion: "relationships/v1",
					ItemFamily: "relationships",
					Name:       "relationship--value1--value2--dependency",
					Data: map[string]any{
						"sourceRef": map[string]any{
							"apiVersion": "resource.custom-platform/v1",
							"family":     "family1",
							"name":       "value2",
						},
						"targetRef": map[string]any{
							"apiVersion": "v1",
							"family":     "family",
							"name":       "item1",
						},
						"type": "dependency",
					},
					OperationTime: "2024-06-01T12:00:00Z",
				},
				{
					APIVersion: "relationships/v1",
					ItemFamily: "relationships",
					Name:       "relationship--value1--value2--dependency-1",
					Data: map[string]any{
						"sourceRef": map[string]any{
							"apiVersion": "resource.custom-platform/v1",
							"family":     "family1",
							"name":       "value2-1",
						},
						"targetRef": map[string]any{
							"apiVersion": "v1",
							"family":     "family",
							"name":       "item1",
						},
						"type": "dependency",
					},
					OperationTime: "2024-06-01T12:00:00Z",
				},
			},
			expectedDeletion: nil,
			useExtra:         true,
			numExtras:        2,
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
			numExtras := 1
			if test.numExtras > 0 {
				numExtras = test.numExtras
			}
			extra := getMappingsExtra(t, test.useExtra, test.deletePolicy, numExtras)
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
	t.Parallel()
	testCases := map[string]struct {
		source           func(c chan<- struct{}) any
		expectedData     []*destination.Data
		expectedDeletion []*destination.Data
		expectedErr      error
		useExtra         bool
		numExtras        int
		deletePolicy     string
	}{
		"unsupported source error": {
			source: func(c chan<- struct{}) any {
				close(c)
				return "not a valid source"
			},
			expectedErr: errors.ErrUnsupported,
			useExtra:    false,
		},
		"source return an error": {
			source: func(c chan<- struct{}) any {
				close(c)
				return fakesource.NewFakeWebhookSourceWithError(t, assert.AnError)
			},
			expectedErr: assert.AnError,
			useExtra:    false,
		},
		"valid webhook pipeline return mapped data without extra mappings": {
			source: func(c chan<- struct{}) any {
				return fakesource.NewFakeUnclosableWebhookSource(t, http.MethodPost, "/webhook", func(ctx context.Context, _ map[string]source.Extra, dataChan chan<- source.Data) error {
					dataChan <- type1
					dataChan <- type2
					close(c)
					return nil
				})
			},
			expectedData: []*destination.Data{
				{
					APIVersion: "v1",
					ItemFamily: "family",
					Name:       "item1",
					Metadata:   map[string]any{},
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
					ItemFamily:    "family2",
					Name:          "item2",
					OperationTime: "2024-06-01T12:00:00Z",
				},
			},
		},
		"valid webhook pipeline return mapped data with extra mappings": {
			source: func(c chan<- struct{}) any {
				return fakesource.NewFakeUnclosableWebhookSource(t, http.MethodPost, "/webhook", func(ctx context.Context, _ map[string]source.Extra, dataChan chan<- source.Data) error {
					dataChan <- type1
					dataChan <- type2
					close(c)
					return nil
				})
			},
			expectedData: []*destination.Data{
				{
					APIVersion: "v1",
					ItemFamily: "family",
					Name:       "item1",
					Metadata:   map[string]any{},
					Data: map[string]any{
						"field1": "value1",
						"field2": "value2",
					},
					OperationTime: "2024-06-01T12:00:00Z",
				},
				{
					APIVersion: "relationships/v1",
					ItemFamily: "relationships",
					Name:       "relationship--value1--value2--dependency",
					Data: map[string]any{
						"sourceRef": map[string]any{
							"apiVersion": "resource.custom-platform/v1",
							"family":     "family1",
							"name":       "value2",
						},
						"targetRef": map[string]any{
							"apiVersion": "v1",
							"family":     "family",
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
					ItemFamily:    "family2",
					Name:          "item2",
					OperationTime: "2024-06-01T12:00:00Z",
				},
			},
			useExtra: true,
		},
		"valid webhook pipeline return deletion with extra mappings delete cascade": {
			source: func(c chan<- struct{}) any {
				return fakesource.NewFakeUnclosableWebhookSource(t, http.MethodPost, "/webhook", func(ctx context.Context, _ map[string]source.Extra, dataChan chan<- source.Data) error {
					dataChan <- type1D
					dataChan <- deleteExtra
					close(c)
					return nil
				})
			},
			expectedData: nil,
			expectedDeletion: []*destination.Data{
				{
					APIVersion:    "v1",
					ItemFamily:    "family",
					Name:          "item1",
					OperationTime: "2024-06-01T12:00:00Z",
				},
				{
					APIVersion:    "relationships/v1",
					ItemFamily:    "relationships",
					Name:          "relationship--value1--value2--dependency",
					OperationTime: "2024-06-01T12:00:00Z",
				},
			},
			useExtra:     true,
			deletePolicy: "cascade",
		},
		"valid webhook pipeline return mapped data with multiple extra mappings": {
			source: func(c chan<- struct{}) any {
				return fakesource.NewFakeUnclosableWebhookSource(t, http.MethodPost, "/webhook", func(ctx context.Context, _ map[string]source.Extra, dataChan chan<- source.Data) error {
					dataChan <- type1
					close(c)
					return nil
				})
			},
			expectedData: []*destination.Data{
				{
					APIVersion: "v1",
					ItemFamily: "family",
					Name:       "item1",
					Metadata:   map[string]any{},
					Data: map[string]any{
						"field1": "value1",
						"field2": "value2",
					},
					OperationTime: "2024-06-01T12:00:00Z",
				},
				{
					APIVersion: "relationships/v1",
					ItemFamily: "relationships",
					Name:       "relationship--value1--value2--dependency",
					Data: map[string]any{
						"sourceRef": map[string]any{
							"apiVersion": "resource.custom-platform/v1",
							"family":     "family1",
							"name":       "value2",
						},
						"targetRef": map[string]any{
							"apiVersion": "v1",
							"family":     "family",
							"name":       "item1",
						},
						"type": "dependency",
					},
					OperationTime: "2024-06-01T12:00:00Z",
				},
				{
					APIVersion: "relationships/v1",
					ItemFamily: "relationships",
					Name:       "relationship--value1--value2--dependency-1",
					Data: map[string]any{
						"sourceRef": map[string]any{
							"apiVersion": "resource.custom-platform/v1",
							"family":     "family1",
							"name":       "value2-1",
						},
						"targetRef": map[string]any{
							"apiVersion": "v1",
							"family":     "family",
							"name":       "item1",
						},
						"type": "dependency",
					},
					OperationTime: "2024-06-01T12:00:00Z",
				},
			},
			expectedDeletion: nil,
			useExtra:         true,
			numExtras:        2,
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()

			destination := fakedestination.NewFakeDestination(t)

			syncChan := make(chan struct{})
			source := test.source(syncChan)

			numExtras := 1
			if test.numExtras > 0 {
				numExtras = test.numExtras
			}
			extra := getMappingsExtra(t, test.useExtra, test.deletePolicy, numExtras)
			pipeline, err := New(ctx, source, testMappers(t, extra), destination)
			require.NoError(t, err)

			fakeServer := fakeserver.NewFakeServer(t, http.MethodPost, "/webhook")
			pipeline.serverCreator = func(_ context.Context) (server.Server, error) {
				return fakeServer, nil
			}

			go func() {
				<-fakeServer.StartedServer()
				err = fakeServer.CallRegisterWebhook(ctx)
				if test.expectedErr != nil {
					assert.ErrorIs(t, err, test.expectedErr)
					assert.Empty(t, destination.SentData)
					assert.Empty(t, destination.DeletedData)
					return
				}
				assert.NoError(t, err)
				select {
				case <-syncChan:
					assert.NoError(t, fakeServer.Stop(ctx))
				case <-ctx.Done():
					assert.Fail(t, "timeout waiting for pipeline to stop")
				}
			}()

			err = pipeline.Start(ctx)
			if test.expectedErr != nil {
				assert.ErrorIs(t, err, test.expectedErr)
				assert.Empty(t, destination.SentData)
				assert.Empty(t, destination.DeletedData)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, test.expectedData, destination.SentData)
			assert.Equal(t, test.expectedDeletion, destination.DeletedData)
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
					ItemFamily: "family",
					Name:       "item1",
					Metadata:   map[string]any{},
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
					ItemFamily:    "family2",
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
