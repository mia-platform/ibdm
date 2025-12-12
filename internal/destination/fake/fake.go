// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package fake

import (
	"context"
	"testing"

	"github.com/mia-platform/ibdm/internal/destination"
)

var _ destination.Sender = &FakeDestination{}

type SentDataRecord struct {
	Identifier string
	Spec       map[string]any
}

type FakeDestination struct {
	tb testing.TB

	SentData    []SentDataRecord
	DeletedData []string
}

func NewFakeDestination(tb testing.TB) *FakeDestination {
	tb.Helper()
	return &FakeDestination{tb: tb}
}

func (f *FakeDestination) SendData(ctx context.Context, identifier string, spec map[string]any) error {
	f.tb.Helper()
	f.SentData = append(f.SentData, SentDataRecord{Identifier: identifier, Spec: spec})
	return nil
}

func (f *FakeDestination) DeleteData(ctx context.Context, identifier string) error {
	f.tb.Helper()
	f.DeletedData = append(f.DeletedData, identifier)
	return nil
}
