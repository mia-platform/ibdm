// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package pipeline

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/mapper"
	"github.com/mia-platform/ibdm/internal/source"
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
		source           any
		expectedData     []mapper.MappedData
		expectedDeletion []string
		expectedErr      error
	}{
		"unsupported source error": {
			source:      "not a valid source",
			expectedErr: errors.ErrUnsupported,
		},
		"source return an error": {
			source: &fakeSource{
				t:           t,
				returnError: true,
			},
			expectedErr: assert.AnError,
		},
		"valid pipeline return mapped data": {
			source: &fakeSource{
				t:      t,
				events: []source.Data{type1, brokenType, unknownType, type2},
			},
			expectedData: []mapper.MappedData{
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
			destination := &fakeDestination{}
			pipeline := New(tc.source, testMappers, destination)

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()

			err := pipeline.Start(ctx)
			if tc.expectedErr != nil {
				assert.ErrorIs(t, err, tc.expectedErr)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tc.expectedData, destination.receivedData)
			assert.Equal(t, tc.expectedDeletion, destination.deletedData)
		})
	}
}

func TestPipelineCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())

	destination := &fakeDestination{}
	pipeline := New(&hangingFakeSource{t: t}, map[string]mapper.Mapper{}, destination)
	go cancel()

	err := pipeline.Start(ctx)

	assert.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, destination.receivedData)
	assert.Empty(t, destination.deletedData)
}

func TestClosableSource(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
	defer cancel()

	syncChan := make(chan struct{})
	destination := &fakeDestination{}
	pipeline := New(&fakeClosableSource{t: t, started: syncChan}, map[string]mapper.Mapper{}, destination)

	defer close(syncChan)
	go func() {
		err := pipeline.Start(ctx)
		assert.NoError(t, err)
	}()

	<-syncChan
	err := pipeline.Stop(ctx, 2*time.Second)
	assert.NoError(t, err)

	assert.Empty(t, destination.receivedData)
	assert.Empty(t, destination.deletedData)
}

func TestNotClosableSourceStop(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())

	destination := &fakeDestination{}
	pipeline := New(&hangingFakeSource{t: t}, map[string]mapper.Mapper{}, destination)

	go func() {
		err := pipeline.Start(ctx)
		assert.ErrorIs(t, err, context.Canceled)
	}()

	err := pipeline.Stop(ctx, 2*time.Second)
	assert.NoError(t, err)
	cancel()

	assert.Empty(t, destination.receivedData)
	assert.Empty(t, destination.deletedData)
}

var _ DataDestination = &fakeDestination{}

type fakeDestination struct {
	receivedData []mapper.MappedData
	deletedData  []string
}

func (f *fakeDestination) SendData(ctx context.Context, data mapper.MappedData) error {
	f.receivedData = append(f.receivedData, data)
	return nil
}

func (f *fakeDestination) DeleteData(ctx context.Context, id string) error {
	f.deletedData = append(f.deletedData, id)
	return nil
}

var _ source.EventSource = &fakeSource{}

type fakeSource struct {
	t           *testing.T
	returnError bool
	events      []source.Data
}

func (f *fakeSource) StartEventStream(ctx context.Context, types []string, out chan<- source.Data) error {
	f.t.Helper()
	if f.returnError {
		return fmt.Errorf("error for testing: %w", assert.AnError)
	}

	for _, event := range f.events {
		out <- event
	}

	return nil
}

var _ source.EventSource = &hangingFakeSource{}

type hangingFakeSource struct {
	t *testing.T
}

func (h *hangingFakeSource) StartEventStream(ctx context.Context, types []string, out chan<- source.Data) error {
	h.t.Helper()
	<-ctx.Done()
	return ctx.Err()
}

var _ source.EventSource = &fakeClosableSource{}
var _ source.ClosableSource = &fakeClosableSource{}

type fakeClosableSource struct {
	cancel  context.CancelFunc
	started chan<- struct{}
	t       *testing.T
}

func (f *fakeClosableSource) StartEventStream(ctx context.Context, types []string, out chan<- source.Data) error {
	f.t.Helper()
	ctx, cancel := context.WithCancel(ctx)
	f.cancel = cancel

	f.started <- struct{}{}
	<-ctx.Done()
	return nil
}

func (f *fakeClosableSource) Close(_ context.Context, _ time.Duration) error {
	f.t.Helper()
	if f.cancel != nil {
		f.cancel()
	}
	return nil
}
