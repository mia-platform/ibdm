// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package fake

import (
	"context"
	"testing"
	"time"

	"github.com/mia-platform/ibdm/internal/source"
)

// FakeSyncableSource exposes sync and close behaviour for tests.
type FakeSyncableSource interface {
	source.SyncableSource
	source.ClosableSource
}

var _ FakeSyncableSource = &fakeSyncableSource{}

// fakeSyncableSource buffers sync results and supports cancellation.
type fakeSyncableSource struct {
	tb          testing.TB
	syncData    []source.Data
	stopChannel chan struct{}
}

// NewFakeSyncableSource returns a FakeSyncableSource producing syncData.
func NewFakeSyncableSource(tb testing.TB, syncData []source.Data) FakeSyncableSource {
	tb.Helper()

	return &fakeSyncableSource{
		tb:          tb,
		syncData:    syncData,
		stopChannel: make(chan struct{}, 1),
	}
}

// StartSyncProcess emits syncData until completion, cancellation, or close.
func (f *fakeSyncableSource) StartSyncProcess(ctx context.Context, _ map[string]source.Extra, results chan<- source.Data) error {
	f.tb.Helper()

	if ctx.Err() != nil {
		return ctx.Err()
	}

	for _, data := range f.syncData {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-f.stopChannel:
			return nil
		default:
			results <- data
		}
	}

	return nil
}

// Close signals the sync loop to stop.
func (f *fakeSyncableSource) Close(_ context.Context, _ time.Duration) error {
	f.tb.Helper()
	close(f.stopChannel)
	return nil
}
