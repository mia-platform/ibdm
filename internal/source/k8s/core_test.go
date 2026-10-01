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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/mia-platform/ibdm/internal/source"
)

func TestSyncCluster(t *testing.T) {
	setupFixedTime(t)

	testCases := map[string]struct {
		objects   []runtime.Object
		nodeCount int
		nodes     []map[string]any
		totalCPU  float64
	}{
		"no nodes": {nodeCount: 0, nodes: []map[string]any{}},
		"multiple nodes": {
			objects:   []runtime.Object{newNodePtr("node-3"), newNodePtr("node-1"), newNodePtr("node-2")},
			nodeCount: 3,
			nodes: []map[string]any{
				expectedNode("node-1", 0, []map[string]any{}),
				expectedNode("node-2", 0, []map[string]any{}),
				expectedNode("node-3", 0, []map[string]any{}),
			},
		},
		"details, taints and fractional cpu": {
			objects: []runtime.Object{
				detailedNode("node-b", "500m", nil),
				detailedNode("node-a", "4", []corev1.Taint{
					{Key: "dedicated", Value: "gpu", Effect: corev1.TaintEffectNoSchedule},
					{Key: "node.kubernetes.io/unschedulable", Effect: corev1.TaintEffectNoExecute},
				}),
				newNodePtr("node-c"),
			},
			nodeCount: 3,
			nodes: []map[string]any{
				expectedNode("node-a", 4, []map[string]any{
					{"key": "dedicated", "value": "gpu", "effect": "NoSchedule"},
					{"key": "node.kubernetes.io/unschedulable", "value": "", "effect": "NoExecute"},
				}),
				expectedNode("node-b", 0.5, []map[string]any{}),
				expectedNode("node-c", 0, []map[string]any{}),
			},
			totalCPU: 4.5,
		},
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
				"apiServer":     testAPIServer,
				"clusterName":   testClusterName,
				"nodeCount":     tc.nodeCount,
				"nodes":         tc.nodes,
				"totalCPUCores": tc.totalCPU,
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

// detailedNode builds a node with fictional system info, CPU capacity and taints.
func detailedNode(name, cpu string, taints []corev1.Taint) *corev1.Node {
	node := newNodePtr(name)
	node.Spec.Taints = taints
	node.Status.Capacity = corev1.ResourceList{corev1.ResourceCPU: resource.MustParse(cpu)}
	node.Status.NodeInfo = corev1.NodeSystemInfo{
		KubeletVersion: "v1.30.0",
		OSImage:        "Example Linux 1.0",
		KernelVersion:  "6.1.0-example",
		Architecture:   "amd64",
	}
	return node
}

// expectedNode builds the expected per-node map; nodes created with newNodePtr have no system info.
func expectedNode(name string, cpu float64, taints []map[string]any) map[string]any {
	info := map[string]any{
		"name": name, "kubeletVersion": "", "osImage": "", "kernelVersion": "", "architecture": "",
		"cpuCapacity": cpu, "taints": taints,
	}
	if cpu > 0 {
		info["kubeletVersion"] = "v1.30.0"
		info["osImage"] = "Example Linux 1.0"
		info["kernelVersion"] = "6.1.0-example"
		info["architecture"] = "amd64"
	}
	return info
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
