// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package fake

import (
	"context"
	"testing"
	"time"

	"github.com/mia-platform/ibdm/internal/source"
)

type FakeSyncableSource interface {
	source.SyncableSource
	source.ClosableSource
}

var _ FakeSyncableSource = &fakeSyncableSource{}

type fakeSyncableSource struct {
	t           *testing.T
	syncData    []source.Data
	stopChannel chan struct{}
}

func NewFakeSyncableSource(t *testing.T, syncData []source.Data) FakeSyncableSource {
	t.Helper()

	return &fakeSyncableSource{
		t:           t,
		syncData:    syncData,
		stopChannel: make(chan struct{}, 1),
	}
}

func (f *fakeSyncableSource) StartSyncProcess(ctx context.Context, _ []string, results chan<- source.Data) error {
	f.t.Helper()
	defer close(f.stopChannel)

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

func (f *fakeSyncableSource) Close(_ context.Context, _ time.Duration) error {
	f.t.Helper()
	f.stopChannel <- struct{}{}
	return nil
}
