// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package k8s

import (
	"context"
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/mia-platform/ibdm/internal/source"
)

func TestSyncCluster(t *testing.T) {
	setupFixedTime(t)

	testCases := map[string]struct {
		objects   []runtime.Object
		nodeCount int
	}{
		"no nodes":       {nodeCount: 0},
		"multiple nodes": {objects: []runtime.Object{&corev1.Node{}, newNodePtr("node-2"), newNodePtr("node-3")}, nodeCount: 3},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			s := newFakeSource(t, tc.objects...)
			results := make(chan source.Data, 5)

			require.NoError(t, s.syncCluster(t.Context(), results))
			close(results)

			items := collectData(results)
			require.Len(t, items, 1)
			assert.Equal(t, clusterType, items[0].Type)
			assert.Equal(t, source.DataOperationUpsert, items[0].Operation)
			assert.Equal(t, testFixedTime, items[0].Time)
			assert.Equal(t, map[string]any{
				"apiServer":   testAPIServer,
				"clusterName": testClusterName,
				"nodeCount":   tc.nodeCount,
			}, items[0].Values)
		})
	}
}

func TestSyncClusterListError(t *testing.T) {
	t.Parallel()

	s := newHTTPSource(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	results := make(chan source.Data, 1)

	require.ErrorIs(t, s.syncCluster(t.Context(), results), ErrRetrievingAssets)
	assert.Empty(t, results)
}

func newNodePtr(name string) *corev1.Node {
	node := newNode(name)
	return &node
}

func TestSyncNamespaces(t *testing.T) {
	setupFixedTime(t)

	s := newFakeSource(t,
		newNamespace("team-a", map[string]string{"env": "prod"}),
		newNamespace("team-b", nil),
	)
	results := make(chan source.Data, 5)

	require.NoError(t, s.syncNamespaces(t.Context(), results))
	close(results)

	items := collectData(results)
	require.Len(t, items, 2)

	for _, item := range items {
		assert.Equal(t, namespaceType, item.Type)
		assert.Equal(t, source.DataOperationUpsert, item.Operation)
		assert.Equal(t, testFixedTime, item.Time)
		assert.Equal(t, testAPIServer, item.Values["apiServer"])
	}

	first, ok := items[0].Values["namespace"].(map[string]any)
	require.True(t, ok)
	metadata, ok := first["metadata"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "team-a", metadata["name"])
	assert.Equal(t, map[string]any{"env": "prod"}, metadata["labels"])
	assert.NotContains(t, metadata, "managedFields")

	second, ok := items[1].Values["namespace"].(map[string]any)
	require.True(t, ok)
	secondMetadata, ok := second["metadata"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "team-b", secondMetadata["name"])
	assert.NotContains(t, secondMetadata, "labels")
}

func TestSyncNamespacesPagination(t *testing.T) {
	setupFixedTime(t)

	s := newHTTPSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/namespaces", r.URL.Path)
		if r.URL.Query().Get("continue") == "" {
			writeJSON(t, w, corev1.NamespaceList{
				ListMeta: metav1.ListMeta{Continue: "page-2"},
				Items:    []corev1.Namespace{*newNamespace("team-a", nil)},
			})
			return
		}
		writeJSON(t, w, corev1.NamespaceList{Items: []corev1.Namespace{*newNamespace("team-b", nil)}})
	}))
	results := make(chan source.Data, 5)

	require.NoError(t, s.syncNamespaces(t.Context(), results))
	close(results)
	assert.Len(t, collectData(results), 2)
}

func TestSyncNamespacesListError(t *testing.T) {
	t.Parallel()

	s := newHTTPSource(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))

	require.ErrorIs(t, s.syncNamespaces(t.Context(), make(chan source.Data, 1)), ErrRetrievingAssets)
}

func TestSyncNamespacesContextCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	s := newFakeSource(t, newNamespace("team-a", nil))
	require.ErrorIs(t, s.syncNamespaces(ctx, make(chan source.Data)), context.Canceled)
}

func TestSyncNamespacesCanceledWhileSending(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	s := newFakeSource(t, newNamespace("team-a", nil))

	// unbuffered channel with no reader: the send blocks until the context is canceled.
	done := make(chan error, 1)
	go func() { done <- s.syncNamespaces(ctx, make(chan source.Data)) }()
	cancel()

	require.ErrorIs(t, <-done, context.Canceled)
}

func TestListAllNodesPagination(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	s := newHTTPSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/nodes", r.URL.Path)
		assert.Equal(t, strconv.FormatInt(pageSize, 10), r.URL.Query().Get("limit"))
		calls.Add(1)

		switch r.URL.Query().Get("continue") {
		case "":
			writeJSON(t, w, corev1.NodeList{
				ListMeta: metav1.ListMeta{Continue: "page-2"},
				Items:    []corev1.Node{newNode("node-1"), newNode("node-2")},
			})
		case "page-2":
			writeJSON(t, w, corev1.NodeList{Items: []corev1.Node{newNode("node-3")}})
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))

	nodes, err := s.listAllNodes(t.Context())
	require.NoError(t, err)
	require.Len(t, nodes, 3)
	assert.Equal(t, "node-1", nodes[0].Name)
	assert.Equal(t, "node-3", nodes[2].Name)
	assert.EqualValues(t, 2, calls.Load())
}

func TestListAllNodesEmpty(t *testing.T) {
	t.Parallel()

	s := newFakeSource(t)
	nodes, err := s.listAllNodes(t.Context())
	require.NoError(t, err)
	assert.Empty(t, nodes)
}

func TestListAllNodesError(t *testing.T) {
	t.Parallel()

	s := newHTTPSource(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))

	_, err := s.listAllNodes(t.Context())
	require.ErrorIs(t, err, ErrRetrievingAssets)
}

func TestListAllNodesContextCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	s := newFakeSource(t)
	_, err := s.listAllNodes(ctx)
	require.ErrorIs(t, err, ctx.Err())
}
