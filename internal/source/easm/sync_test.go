// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package easm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/source"
)

// runSync drives StartSyncProcess to completion and returns the emitted data
// and the returned error.
func runSync(t *testing.T, s *Source, typesToSync map[string]source.Extra) ([]source.Data, error) {
	t.Helper()

	ch := make(chan source.Data, 100)
	var data []source.Data

	done := make(chan struct{})
	go func() {
		defer close(done)
		data = collectData(t, ch)
	}()

	err := s.StartSyncProcess(t.Context(), typesToSync, ch)
	close(ch)
	<-done

	return data, err
}

// writeItems encodes a JSON array of items as the /data response body.
func writeItems(w http.ResponseWriter, items []map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(items)
}

func TestStartSyncProcessRouting(t *testing.T) {
	t.Parallel()

	items := []map[string]any{
		{"id": "1", "type": domainType},
		{"id": "2", "type": hostType},
		{"id": "3", "type": ipType},
		{"id": "4", "type": endpointType},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeItems(w, items)
	})
	s := newTestSource(t, handler)

	typesToSync := map[string]source.Extra{
		domainType: {},
		ipType:     {},
	}

	data, err := runSync(t, s, typesToSync)
	require.NoError(t, err)

	// Only domain and ip are requested; host and endpoint are filtered out.
	require.Len(t, data, 2)
	for _, d := range data {
		assert.Equal(t, source.DataOperationUpsert, d.Operation)
		assert.Equal(t, testTime, d.Time)
	}

	expected := []source.Data{
		{Type: domainType, Operation: source.DataOperationUpsert, Values: items[0], Time: testTime},
		{Type: ipType, Operation: source.DataOperationUpsert, Values: items[2], Time: testTime},
	}
	assert.ElementsMatch(t, expected, data)
}

func TestStartSyncProcessPagination(t *testing.T) {
	t.Parallel()

	page1 := []map[string]any{
		{"id": "1", "type": domainType},
		{"id": "2", "type": hostType},
	}
	page2 := []map[string]any{
		{"id": "3", "type": ipType},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get(cursorQueryParam) {
		case "":
			w.Header().Set(nextCursorHeader, "page2")
			writeItems(w, page1)
		case "page2":
			writeItems(w, page2)
		default:
			t.Errorf("unexpected cursor %q", r.URL.Query().Get(cursorQueryParam))
		}
	})
	s := newTestSource(t, handler)

	typesToSync := map[string]source.Extra{
		domainType: {},
		hostType:   {},
		ipType:     {},
	}

	data, err := runSync(t, s, typesToSync)
	require.NoError(t, err)

	// All three items, across both pages, are collected.
	require.Len(t, data, 3)
	expected := []source.Data{
		{Type: domainType, Operation: source.DataOperationUpsert, Values: page1[0], Time: testTime},
		{Type: hostType, Operation: source.DataOperationUpsert, Values: page1[1], Time: testTime},
		{Type: ipType, Operation: source.DataOperationUpsert, Values: page2[0], Time: testTime},
	}
	assert.ElementsMatch(t, expected, data)
}

func TestStartSyncProcessSkipsItemsWithoutType(t *testing.T) {
	t.Parallel()

	items := []map[string]any{
		{"id": "1"},                     // no type field
		{"id": "2", "type": ""},         // empty type
		{"id": "3", "type": domainType}, // valid
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeItems(w, items)
	})
	s := newTestSource(t, handler)

	data, err := runSync(t, s, map[string]source.Extra{domainType: {}})
	require.NoError(t, err)

	require.Len(t, data, 1)
	assert.Equal(t, domainType, data[0].Type)
	assert.Equal(t, items[2], data[0].Values)
}

func TestStartSyncProcessFetchError(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	})
	s := newTestSource(t, handler)

	data, err := runSync(t, s, map[string]source.Extra{domainType: {}})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrEASMSource)
	assert.Empty(t, data)
}

func TestStartSyncProcessAlreadyRunning(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("no request should be made when the sync lock is already held")
	})
	s := newTestSource(t, handler)

	// Simulate an in-flight sync by holding the lock.
	s.syncLock.Lock()
	defer s.syncLock.Unlock()

	data, err := runSync(t, s, map[string]source.Extra{domainType: {}})
	assert.NoError(t, err)
	assert.Empty(t, data)
}

func TestContextCancellationInSync(t *testing.T) {
	t.Parallel()

	// Handler always advertises another page, so the loop would run forever
	// unless the context cancellation breaks it.
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(nextCursorHeader, "next")
		writeItems(w, []map[string]any{{"id": "1", "type": domainType}})
	})
	s := newTestSource(t, handler)

	ctx, cancel := context.WithCancel(t.Context())

	ch := make(chan source.Data, 100)
	done := make(chan error, 1)
	go func() {
		done <- s.StartSyncProcess(ctx, map[string]source.Extra{domainType: {}}, ch)
		close(ch)
	}()

	// Read one item, then cancel.
	<-ch
	cancel()

	err := <-done
	assert.NoError(t, err)
}

func TestHandleErr(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		err       error
		expectNil bool
	}{
		"nil error": {
			err:       nil,
			expectNil: true,
		},
		"context canceled": {
			err:       context.Canceled,
			expectNil: true,
		},
		"wrapped context canceled": {
			err:       fmt.Errorf("fetch failed: %w", context.Canceled),
			expectNil: true,
		},
		"regular error": {
			err: errors.New("something failed"),
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			result := handleErr(tc.err)
			if tc.expectNil {
				assert.NoError(t, result)
				return
			}

			require.Error(t, result)
			assert.ErrorIs(t, result, ErrEASMSource)
		})
	}
}
