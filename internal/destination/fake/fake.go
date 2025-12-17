// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package fake

import (
	"context"
	"testing"

	"github.com/mia-platform/ibdm/internal/destination"
)

var _ destination.Sender = &FakeDestination{}

type FakeDestination struct {
	tb testing.TB

	SentData    []*destination.Data
	DeletedData []*destination.Data
}

func NewFakeDestination(tb testing.TB) *FakeDestination {
	tb.Helper()
	return &FakeDestination{tb: tb}
}

func (f *FakeDestination) SendData(ctx context.Context, data *destination.Data) error {
	f.tb.Helper()
	f.SentData = append(f.SentData, data)
	return nil
}

func (f *FakeDestination) DeleteData(ctx context.Context, data *destination.Data) error {
	f.tb.Helper()
	f.DeletedData = append(f.DeletedData, data)
	return nil
}
