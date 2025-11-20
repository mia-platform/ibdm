// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/mia-platform/ibdm/internal/mapper"
)

const (
	type1 = "test-type-1"
	type2 = "test-type-2"
	type3 = "unknown-type"
	type4 = "brocken-type"
)

var (
	type1MockedData = mapper.MappedData{
		Identifier: type1,
		Spec:       map[string]any{"field": "value1"},
	}
	type2MockedData = mapper.MappedData{
		Identifier: type2,
		Spec:       map[string]any{"field": "value2"},
	}
)

func TestPipeline(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
	defer cancel()

	mappers := map[string]mapper.Mapper{
		type1: NewTestMapper(t, type1MockedData, nil),
		type2: NewTestMapper(t, type2MockedData, nil),
		type4: NewTestMapper(t, mapper.MappedData{}, assert.AnError),
	}

	channel := make(chan Data, 100)
	defer close(channel)
	destination := &testDestination{
		t: t,
	}

	for _, dataType := range []string{type4, type1, type3, type2} {
		channel <- Data{
			Type: dataType,
			Data: map[string]any{"raw": "data1"},
		}
	}

	pipeline := New(channel, mappers, destination)
	pipeline.Run(ctx)
	assert.Equal(t, []mapper.MappedData{type1MockedData, type2MockedData}, destination.writtenData)
}

func TestCancelledContextStopsPipeline(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())

	mappers := map[string]mapper.Mapper{
		type1: NewTestMapper(t, type1MockedData, nil),
	}
	channel := make(chan Data)
	destination := &testDestination{
		t: t,
	}

	go func() {
		cancel()
		channel <- Data{
			Type: type1,
			Data: map[string]any{"raw": "data1"},
		}
		close(channel)
	}()

	pipeline := New(channel, mappers, destination)
	pipeline.Run(ctx)
	assert.Empty(t, destination.writtenData)
}

func TestCloseChannelStopsPipeline(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
	defer cancel()

	mappers := map[string]mapper.Mapper{
		type1: NewTestMapper(t, type1MockedData, nil),
	}
	channel := make(chan Data)
	destination := &testDestination{
		t: t,
	}

	go func() {
		channel <- Data{
			Type: type1,
			Data: map[string]any{"raw": "data1"},
		}

		close(channel)
	}()

	pipeline := New(channel, mappers, destination)
	pipeline.Run(ctx)
	assert.Equal(t, []mapper.MappedData{type1MockedData}, destination.writtenData)
}

func TestErrorInDestinationPipelineContinues(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
	defer cancel()

	mappers := map[string]mapper.Mapper{
		type1: NewTestMapper(t, type1MockedData, nil),
		type2: NewTestMapper(t, type2MockedData, nil),
	}
	channel := make(chan Data, 100)
	defer close(channel)
	destination := &testDestination{
		t:   t,
		err: assert.AnError,
	}

	for _, dataType := range []string{type1, type2} {
		channel <- Data{
			Type: dataType,
			Data: map[string]any{"raw": "data1"},
		}
	}

	pipeline := New(channel, mappers, destination)
	pipeline.Run(ctx)
	assert.Empty(t, destination.writtenData)
}

type testMapper struct {
	t          *testing.T
	mappedData mapper.MappedData
	err        error
}

func NewTestMapper(t *testing.T, mappedData mapper.MappedData, err error) *testMapper {
	t.Helper()
	return &testMapper{
		t:          t,
		mappedData: mappedData,
		err:        err,
	}
}

func (m *testMapper) ApplyTemplates(_ map[string]any) (mapper.MappedData, error) {
	m.t.Helper()
	return m.mappedData, m.err
}

type testDestination struct {
	t           *testing.T
	writtenData []mapper.MappedData
	err         error
}

func (d *testDestination) Write(_ context.Context, data mapper.MappedData) error {
	d.t.Helper()
	if d.err != nil {
		return d.err
	}

	d.writtenData = append(d.writtenData, data)
	return nil
}
