// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package sysdig

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/source"
)

func TestNewSource(t *testing.T) {
	tests := map[string]struct {
		envVars   map[string]string
		expectErr bool
	}{
		"valid configuration": {
			envVars: map[string]string{
				"SYSDIG_URL":       "https://secure.sysdig.com",
				"SYSDIG_API_TOKEN": "test-token",
			},
		},
		"missing URL": {
			envVars: map[string]string{
				"SYSDIG_API_TOKEN": "test-token",
			},
			expectErr: true,
		},
		"missing token": {
			envVars: map[string]string{
				"SYSDIG_URL": "https://secure.sysdig.com",
			},
			expectErr: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			for k, v := range tc.envVars {
				t.Setenv(k, v)
			}

			src, err := NewSource()
			if tc.expectErr {
				assert.Error(t, err)
				assert.ErrorIs(t, err, ErrSysdigSource)
				assert.Nil(t, src)
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, src)
		})
	}
}

func TestStartSyncProcess(t *testing.T) {
	t.Parallel()

	fixedTime := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	originalTimeSource := timeSource
	t.Cleanup(func() { timeSource = originalTimeSource })
	timeSource = func() time.Time { return fixedTime }

	tests := map[string]struct {
		typesToSync map[string]source.Extra
		pages       []sysqlResponse
		expectData  []source.Data
	}{
		"single vulnerability type": {
			typesToSync: map[string]source.Extra{
				"vulnerability": nil,
			},
			pages: []sysqlResponse{
				{
					Data: sysqlData{Items: []map[string]any{
						{
							"vuln": map[string]any{"name": "CVE-2024-0001", "severity": "High"},
							"img":  map[string]any{"imageId": "sha256:abc", "imageReference": "nginx:latest"},
						},
					}},
					Summary: sysqlSummary{FetchedItemsCount: 1},
				},
				{
					Data:    sysqlData{Items: []map[string]any{}},
					Summary: sysqlSummary{FetchedItemsCount: 0},
				},
			},
			expectData: []source.Data{
				{
					Type:      "vulnerability",
					Operation: source.DataOperationUpsert,
					Time:      fixedTime,
					Values: map[string]any{
						"vuln": map[string]any{"name": "CVE-2024-0001", "severity": "High"},
						"img":  map[string]any{"imageId": "sha256:abc", "imageReference": "nginx:latest"},
					},
				},
			},
		},
		"unknown type is skipped": {
			typesToSync: map[string]source.Extra{
				"unknown-type": nil,
			},
			pages:      nil,
			expectData: nil,
		},
		"mixed known and unknown types": {
			typesToSync: map[string]source.Extra{
				"vulnerability":  nil,
				"something-else": nil,
			},
			pages: []sysqlResponse{
				{
					Data: sysqlData{Items: []map[string]any{
						{
							"vuln": map[string]any{"name": "CVE-2024-0002", "severity": "Low"},
							"img":  map[string]any{"imageId": "sha256:def", "imageReference": "alpine:3.18"},
						},
					}},
					Summary: sysqlSummary{FetchedItemsCount: 1},
				},
				{
					Data:    sysqlData{Items: []map[string]any{}},
					Summary: sysqlSummary{FetchedItemsCount: 0},
				},
			},
			expectData: []source.Data{
				{
					Type:      "vulnerability",
					Operation: source.DataOperationUpsert,
					Time:      fixedTime,
					Values: map[string]any{
						"vuln": map[string]any{"name": "CVE-2024-0002", "severity": "Low"},
						"img":  map[string]any{"imageId": "sha256:def", "imageReference": "alpine:3.18"},
					},
				},
			},
		},
		"empty results": {
			typesToSync: map[string]source.Extra{
				"vulnerability": nil,
			},
			pages: []sysqlResponse{
				{
					Data:    sysqlData{Items: []map[string]any{}},
					Summary: sysqlSummary{FetchedItemsCount: 0},
				},
			},
			expectData: nil,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var pageIndex atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				idx := int(pageIndex.Add(1)) - 1
				if tc.pages == nil || idx >= len(tc.pages) {
					t.Error("unexpected API request")
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				err := json.NewEncoder(w).Encode(tc.pages[idx])
				assert.NoError(t, err)
			}))
			t.Cleanup(server.Close)

			src := &Source{
				config: config{
					URL:         server.URL,
					APIToken:    "test-token",
					HTTPTimeout: 10 * time.Second,
					PageSize:    100,
				},
				client: server.Client(),
			}

			results := make(chan source.Data, 100)
			err := src.StartSyncProcess(t.Context(), tc.typesToSync, results)
			close(results)

			require.NoError(t, err)

			var got []source.Data
			for d := range results {
				got = append(got, d)
			}

			assert.Len(t, got, len(tc.expectData))
			for i, expected := range tc.expectData {
				assert.Equal(t, expected.Type, got[i].Type)
				assert.Equal(t, expected.Operation, got[i].Operation)
				assert.Equal(t, expected.Time, got[i].Time)
				assert.Equal(t, expected.Values, got[i].Values)
			}
		})
	}
}

func TestStartSyncProcessContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	src := &Source{
		config: config{
			URL:         "http://localhost",
			APIToken:    "test-token",
			HTTPTimeout: 10 * time.Second,
			PageSize:    100,
		},
		client: http.DefaultClient,
	}

	results := make(chan source.Data, 100)
	err := src.StartSyncProcess(ctx, map[string]source.Extra{"vulnerability": nil}, results)
	close(results)

	assert.NoError(t, err)
	assert.Empty(t, results)
}

func TestStartSyncProcessConcurrencyGuard(t *testing.T) {
	t.Parallel()

	blockCh := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-blockCh
		resp := sysqlResponse{
			Data:    sysqlData{Items: []map[string]any{}},
			Summary: sysqlSummary{FetchedItemsCount: 0},
		}
		w.Header().Set("Content-Type", "application/json")
		err := json.NewEncoder(w).Encode(resp)
		assert.NoError(t, err)
	}))
	t.Cleanup(server.Close)

	src := &Source{
		config: config{
			URL:         server.URL,
			APIToken:    "test-token",
			HTTPTimeout: 10 * time.Second,
			PageSize:    100,
		},
		client: server.Client(),
	}

	results := make(chan source.Data, 100)
	typesToSync := map[string]source.Extra{"vulnerability": nil}

	var wg sync.WaitGroup

	// Start first sync that blocks on the server.
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = src.StartSyncProcess(t.Context(), typesToSync, results)
	}()

	// Give the first goroutine time to acquire the lock.
	// We use a short spin-wait instead of time.Sleep.
	require.Eventually(t, func() bool {
		return !src.syncLock.TryLock()
	}, 5*time.Second, 10*time.Millisecond, "first sync should have acquired the lock")

	// Second sync should return immediately due to lock.
	err := src.StartSyncProcess(t.Context(), typesToSync, results)
	assert.NoError(t, err)

	// Unblock the first sync.
	close(blockCh)
	wg.Wait()
}

func TestStartSyncProcessAPIError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, err := w.Write([]byte("forbidden"))
		assert.NoError(t, err)
	}))
	t.Cleanup(server.Close)

	src := &Source{
		config: config{
			URL:         server.URL,
			APIToken:    "test-token",
			HTTPTimeout: 10 * time.Second,
			PageSize:    100,
		},
		client: server.Client(),
	}

	results := make(chan source.Data, 100)
	err := src.StartSyncProcess(t.Context(), map[string]source.Extra{"vulnerability": nil}, results)
	close(results)

	// Per-type errors are logged but don't abort the sync — no error returned.
	assert.NoError(t, err)
	assert.Empty(t, results)
}
