// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package k8s

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/mia-platform/ibdm/internal/source"
)

func TestStartSyncProcessDispatch(t *testing.T) {
	setupFixedTime(t)

	testCases := map[string]struct {
		types       map[string]source.Extra
		expectTypes []string
	}{
		"cluster only":   {types: map[string]source.Extra{clusterType: {}}, expectTypes: []string{clusterType}},
		"namespace only": {types: map[string]source.Extra{namespaceType: {}}, expectTypes: []string{namespaceType, namespaceType}},
		"both types": {
			types:       map[string]source.Extra{clusterType: {}, namespaceType: {}},
			expectTypes: []string{clusterType, namespaceType, namespaceType},
		},
		"unknown types are skipped": {types: map[string]source.Extra{"node": {}, "unknown": {}}},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			s := newFakeSource(t, newNamespace("team-a", nil), newNamespace("team-b", nil), &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}})
			results := make(chan source.Data, 10)

			require.NoError(t, s.StartSyncProcess(t.Context(), tc.types, results))
			close(results)

			items := collectData(results)
			if tc.expectTypes == nil {
				assert.Empty(t, items)
				return
			}

			gotTypes := make([]string, 0, len(items))
			for _, item := range items {
				gotTypes = append(gotTypes, item.Type)
			}
			assert.Equal(t, tc.expectTypes, gotTypes)
		})
	}
}

func TestStartSyncProcessErrorIsolation(t *testing.T) {
	setupFixedTime(t)

	// nodes are forbidden, namespaces are served: the namespace type must still be synced.
	s := newHTTPSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/namespaces" {
			writeJSON(t, w, corev1.NamespaceList{Items: []corev1.Namespace{*newNamespace("team-a", nil)}})
			return
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	results := make(chan source.Data, 10)

	err := s.StartSyncProcess(t.Context(), map[string]source.Extra{clusterType: {}, namespaceType: {}}, results)
	require.ErrorIs(t, err, ErrK8sSource)
	require.ErrorIs(t, err, ErrRetrievingAssets)
	close(results)

	items := collectData(results)
	require.Len(t, items, 1)
	assert.Equal(t, namespaceType, items[0].Type)
}

func TestStartSyncProcessLock(t *testing.T) {
	t.Parallel()

	s := newFakeSource(t, newNamespace("team-a", nil))
	s.syncLock.Lock()
	t.Cleanup(s.syncLock.Unlock)

	results := make(chan source.Data, 10)
	require.NoError(t, s.StartSyncProcess(t.Context(), map[string]source.Extra{namespaceType: {}}, results))
	assert.Empty(t, results)
}

func TestStartSyncProcessContextCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	s := newFakeSource(t, newNamespace("team-a", nil))
	results := make(chan source.Data, 10)

	require.NoError(t, s.StartSyncProcess(ctx, map[string]source.Extra{clusterType: {}, namespaceType: {}}, results))
	assert.Empty(t, results)
}

func TestStartSyncProcessCanceledDuringType(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	s := newFakeSource(t, newNamespace("team-a", nil))

	done := make(chan error, 1)
	go func() {
		done <- s.StartSyncProcess(ctx, map[string]source.Extra{namespaceType: {}}, make(chan source.Data))
	}()
	cancel()

	require.NoError(t, <-done)
}
