// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package sysdig

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecuteSysQLQuery(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		handler    http.HandlerFunc
		expectErr  bool
		expectResp *sysqlResponse
	}{
		"successful query": {
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))

				var req sysqlRequest
				require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
				assert.Contains(t, req.Query, "MATCH Image")

				resp := sysqlResponse{
					Data: sysqlData{
						Items: []map[string]any{
							{
								"vuln": map[string]any{"name": "CVE-2024-0001", "severity": "High"},
								"img":  map[string]any{"imageId": "sha256:abc123", "imageReference": "nginx:latest"},
							},
						},
					},
					Summary: sysqlSummary{FetchedItemsCount: 1},
				}
				w.Header().Set("Content-Type", "application/json")
				err := json.NewEncoder(w).Encode(resp)
				require.NoError(t, err)
			},
			expectResp: &sysqlResponse{
				Data: sysqlData{
					Items: []map[string]any{
						{
							"vuln": map[string]any{"name": "CVE-2024-0001", "severity": "High"},
							"img":  map[string]any{"imageId": "sha256:abc123", "imageReference": "nginx:latest"},
						},
					},
				},
				Summary: sysqlSummary{FetchedItemsCount: 1},
			},
		},
		"non-2xx response": {
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, err := w.Write([]byte("internal error"))
				require.NoError(t, err)
			},
			expectErr: true,
		},
		"invalid JSON response": {
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, err := w.Write([]byte("not json"))
				require.NoError(t, err)
			},
			expectErr: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(tc.handler)
			t.Cleanup(server.Close)

			resp, err := executeSysQLQuery(t.Context(), server.Client(), server.URL, "test-token", "MATCH Image AS img AFFECTED_BY Vulnerability AS vuln RETURN img, vuln LIMIT 100 OFFSET 0;")
			if tc.expectErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expectResp.Summary.FetchedItemsCount, resp.Summary.FetchedItemsCount)
			assert.Len(t, resp.Data.Items, len(tc.expectResp.Data.Items))
		})
	}
}

func TestExecuteSysQLQueryContextCancelled(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		err := json.NewEncoder(w).Encode(sysqlResponse{})
		assert.NoError(t, err)
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := executeSysQLQuery(ctx, server.Client(), server.URL, "test-token", "MATCH Image RETURN img;")
	assert.Error(t, err)
}

func TestQueryAllPages(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		pages           []sysqlResponse
		expectItemCount int
		expectErr       bool
	}{
		"single page with results": {
			pages: []sysqlResponse{
				{
					Data: sysqlData{Items: []map[string]any{
						{"vuln": map[string]any{"name": "CVE-1"}, "img": map[string]any{"imageId": "img1"}},
						{"vuln": map[string]any{"name": "CVE-2"}, "img": map[string]any{"imageId": "img2"}},
					}},
					Summary: sysqlSummary{FetchedItemsCount: 2},
				},
				{
					Data:    sysqlData{Items: []map[string]any{}},
					Summary: sysqlSummary{FetchedItemsCount: 0},
				},
			},
			expectItemCount: 2,
		},
		"multiple pages": {
			pages: []sysqlResponse{
				{
					Data: sysqlData{Items: []map[string]any{
						{"vuln": map[string]any{"name": "CVE-1"}, "img": map[string]any{"imageId": "img1"}},
					}},
					Summary: sysqlSummary{FetchedItemsCount: 1},
				},
				{
					Data: sysqlData{Items: []map[string]any{
						{"vuln": map[string]any{"name": "CVE-2"}, "img": map[string]any{"imageId": "img2"}},
					}},
					Summary: sysqlSummary{FetchedItemsCount: 1},
				},
				{
					Data:    sysqlData{Items: []map[string]any{}},
					Summary: sysqlSummary{FetchedItemsCount: 0},
				},
			},
			expectItemCount: 2,
		},
		"empty first page": {
			pages: []sysqlResponse{
				{
					Data:    sysqlData{Items: []map[string]any{}},
					Summary: sysqlSummary{FetchedItemsCount: 0},
				},
			},
			expectItemCount: 0,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var pageIndex atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				idx := int(pageIndex.Add(1)) - 1
				require.Less(t, idx, len(tc.pages), "unexpected extra page request")

				w.Header().Set("Content-Type", "application/json")
				err := json.NewEncoder(w).Encode(tc.pages[idx])
				assert.NoError(t, err)
			}))
			t.Cleanup(server.Close)

			var totalItems int
			err := queryAllPages(t.Context(), server.Client(), server.URL, "test-token", "MATCH Image AS img AFFECTED_BY Vulnerability AS vuln RETURN img, vuln", 100, func(items []map[string]any) error {
				totalItems += len(items)
				return nil
			})

			if tc.expectErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expectItemCount, totalItems)
		})
	}
}

func TestQueryAllPagesContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())

	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count := requestCount.Add(1)
		if count == 2 {
			cancel()
		}
		resp := sysqlResponse{
			Data: sysqlData{Items: []map[string]any{
				{"vuln": map[string]any{"name": "CVE-1"}, "img": map[string]any{"imageId": "img1"}},
			}},
			Summary: sysqlSummary{FetchedItemsCount: 1},
		}
		w.Header().Set("Content-Type", "application/json")
		encodeErr := json.NewEncoder(w).Encode(resp)
		assert.NoError(t, encodeErr)
	}))
	t.Cleanup(server.Close)

	var totalItems int
	err := queryAllPages(ctx, server.Client(), server.URL, "test-token", "MATCH Image AS img AFFECTED_BY Vulnerability AS vuln RETURN img, vuln", 1, func(items []map[string]any) error {
		totalItems += len(items)
		return nil
	})

	assert.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	// Should have processed at most 2 pages before cancellation kicks in.
	assert.LessOrEqual(t, totalItems, 2)
}

func TestQueryAllPagesServerError(t *testing.T) {
	t.Parallel()

	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count := requestCount.Add(1)
		if count == 2 {
			w.WriteHeader(http.StatusInternalServerError)
			_, err := w.Write([]byte("server error"))
			assert.NoError(t, err)
			return
		}
		resp := sysqlResponse{
			Data: sysqlData{Items: []map[string]any{
				{"vuln": map[string]any{"name": "CVE-1"}, "img": map[string]any{"imageId": "img1"}},
			}},
			Summary: sysqlSummary{FetchedItemsCount: 1},
		}
		w.Header().Set("Content-Type", "application/json")
		encodeErr := json.NewEncoder(w).Encode(resp)
		assert.NoError(t, encodeErr)
	}))
	t.Cleanup(server.Close)

	err := queryAllPages(t.Context(), server.Client(), server.URL, "test-token", "MATCH Image AS img AFFECTED_BY Vulnerability AS vuln RETURN img, vuln", 1, func(_ []map[string]any) error {
		return nil
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}
