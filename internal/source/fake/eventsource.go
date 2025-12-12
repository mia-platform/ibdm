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

var _ source.EventSource = &unclosableEventSource{}

type unclosableEventSource struct {
	tb testing.TB

	eventsData     []source.Data
	streamFinished chan<- struct{}
	stopChannel    chan struct{}
}

var _ FakeEventSource = &fakeEventSource{}

type fakeEventSource struct {
	*unclosableEventSource
}

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

func NewFakeUnclosableEventSource(tb testing.TB, eventsData []source.Data, streamFinished chan<- struct{}) source.EventSource {
	tb.Helper()

	return &unclosableEventSource{
		tb:             tb,
		eventsData:     eventsData,
		streamFinished: streamFinished,
	}
}

func (f *unclosableEventSource) StartEventStream(ctx context.Context, _ []string, results chan<- source.Data) error {
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

func (f *fakeEventSource) Close(_ context.Context, _ time.Duration) error {
	f.tb.Helper()
	close(f.stopChannel)
	return nil
}

type ErrorEventSource interface {
	source.EventSource
	source.SyncableSource
}

var _ ErrorEventSource = &errorEventSource{}

type errorEventSource struct {
	tb  testing.TB
	err error
}

func NewFakeSourceWithError(tb testing.TB, err error) ErrorEventSource {
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

func (f *errorEventSource) StartSyncProcess(_ context.Context, _ []string, _ chan<- source.Data) error {
	f.tb.Helper()
	return f.err
}
