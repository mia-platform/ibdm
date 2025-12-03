// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package fake

import (
	"context"
	"testing"
	"time"

	"github.com/mia-platform/ibdm/internal/source"
)

type FakeEventSource interface {
	source.EventSource
	source.ClosableSource
}

var _ FakeEventSource = &fakeEventSource{}

type fakeEventSource struct {
	t *testing.T

	eventsData     []source.Data
	streamFinished chan<- struct{}
	stopChannel    chan struct{}
}

func NewFakeEventSource(t *testing.T, eventsData []source.Data, streamFinished chan<- struct{}) FakeEventSource {
	t.Helper()

	return &fakeEventSource{
		t:              t,
		eventsData:     eventsData,
		streamFinished: streamFinished,
		stopChannel:    make(chan struct{}, 1),
	}
}

func (f *fakeEventSource) StartEventStream(ctx context.Context, _ []string, results chan<- source.Data) error {
	f.t.Helper()
	defer close(f.stopChannel)

	for _, data := range f.eventsData {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			results <- data
		}
	}

	f.streamFinished <- struct{}{}
	<-f.stopChannel
	return nil
}

func (f *fakeEventSource) Close(_ context.Context, _ time.Duration) error {
	f.t.Helper()
	f.stopChannel <- struct{}{}
	return nil
}

var _ source.EventSource = &errorEventSource{}

type errorEventSource struct {
	t   *testing.T
	err error
}

func NewFakeEventSourceWithError(t *testing.T, err error) source.EventSource {
	t.Helper()

	return &errorEventSource{
		t:   t,
		err: err,
	}
}

func (f *errorEventSource) StartEventStream(_ context.Context, _ []string, _ chan<- source.Data) error {
	f.t.Helper()
	return f.err
}
