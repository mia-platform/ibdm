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
	tb testing.TB

	eventsData     []source.Data
	streamFinished chan<- struct{}
	stopChannel    chan struct{}
}

func NewFakeEventSource(tb testing.TB, eventsData []source.Data, streamFinished chan<- struct{}) FakeEventSource {
	tb.Helper()

	return &fakeEventSource{
		tb:             tb,
		eventsData:     eventsData,
		streamFinished: streamFinished,
		stopChannel:    make(chan struct{}, 1),
	}
}

func (f *fakeEventSource) StartEventStream(ctx context.Context, _ []string, results chan<- source.Data) error {
	f.tb.Helper()
	defer close(f.stopChannel)

	if ctx.Err() != nil {
		return ctx.Err()
	}

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
	f.tb.Helper()
	f.stopChannel <- struct{}{}
	return nil
}

var _ source.EventSource = &errorEventSource{}

type errorEventSource struct {
	tb  testing.TB
	err error
}

func NewFakeEventSourceWithError(tb testing.TB, err error) source.EventSource {
	tb.Helper()

	return &errorEventSource{
		tb:  tb,
		err: err,
	}
}

func (f *errorEventSource) StartEventStream(_ context.Context, _ []string, _ chan<- source.Data) error {
	f.tb.Helper()
	return f.err
}

type hangingEventSource struct {
	tb testing.TB
}

func NewHangingEventSource(tb testing.TB) source.EventSource {
	tb.Helper()

	return &hangingEventSource{
		tb: tb,
	}
}

func (h *hangingEventSource) StartEventStream(ctx context.Context, _ []string, _ chan<- source.Data) error {
	h.tb.Helper()
	<-ctx.Done()
	return ctx.Err()
}
