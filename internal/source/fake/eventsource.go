// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package fake

import (
	"context"
	"testing"
	"time"

	"github.com/mia-platform/ibdm/internal/source"
)

// FakeEventSource combines event streaming and closing behaviour.
type FakeEventSource interface {
	source.EventSource
	source.ClosableSource
}

var _ source.EventSource = &unclosableEventSource{}

// unclosableEventSource simulates an EventSource without close support.
type unclosableEventSource struct {
	tb testing.TB

	eventsData     []source.Data
	streamFinished chan<- struct{}
	stopChannel    chan struct{}
}

var _ FakeEventSource = &fakeEventSource{}

// fakeEventSource wraps an unclosableEventSource with a Close implementation.
type fakeEventSource struct {
	*unclosableEventSource
}

// NewFakeEventSource returns a closable fake event source.
func NewFakeEventSource(tb testing.TB, eventsData []source.Data, streamFinished chan<- struct{}) FakeEventSource {
	tb.Helper()

	return &fakeEventSource{
		unclosableEventSource: &unclosableEventSource{
			tb:             tb,
			eventsData:     eventsData,
			streamFinished: streamFinished,
			stopChannel:    make(chan struct{}, 1),
		},
	}
}

// NewFakeUnclosableEventSource returns an EventSource without close capabilities.
func NewFakeUnclosableEventSource(tb testing.TB, eventsData []source.Data, streamFinished chan<- struct{}) source.EventSource {
	tb.Helper()

	return &unclosableEventSource{
		tb:             tb,
		eventsData:     eventsData,
		streamFinished: streamFinished,
	}
}

// StartEventStream pushes queued events and blocks until Close is invoked or the context ends.
func (f *unclosableEventSource) StartEventStream(ctx context.Context, _ map[string]source.Extra, results chan<- source.Data) error {
	f.tb.Helper()

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
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-f.stopChannel:
			return nil
		}
	}
}

// Close signals the stream to exit.
func (f *fakeEventSource) Close(_ context.Context, _ time.Duration) error {
	f.tb.Helper()
	close(f.stopChannel)
	return nil
}

// ErrorEventSource combines event and sync flows while always returning an error.
type ErrorEventSource interface {
	source.EventSource
	source.SyncableSource
}

var _ ErrorEventSource = &errorEventSource{}

// errorEventSource returns a configured error for every call.
type errorEventSource struct {
	tb  testing.TB
	err error
}

// NewFakeSourceWithError builds a source that always returns err.
func NewFakeSourceWithError(tb testing.TB, err error) ErrorEventSource {
	tb.Helper()

	return &errorEventSource{
		tb:  tb,
		err: err,
	}
}

// StartEventStream satisfies the EventSource interface returning the configured error.
func (f *errorEventSource) StartEventStream(_ context.Context, _ map[string]source.Extra, _ chan<- source.Data) error {
	f.tb.Helper()
	return f.err
}

// StartSyncProcess satisfies the SyncableSource interface returning the configured error.
func (f *errorEventSource) StartSyncProcess(_ context.Context, _ map[string]source.Extra, _ chan<- source.Data) error {
	f.tb.Helper()
	return f.err
}
