// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package k8s

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"github.com/mia-platform/ibdm/internal/source"
)

const (
	testAPIServer   = "https://api.my-cluster.example.com:6443"
	testClusterName = "my-cluster"
)

// testFixedTime is the canonical fixed time used across all time-sensitive tests.
var testFixedTime = time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)

func setupFixedTime(t *testing.T) {
	t.Helper()
	originalTimeSource := timeSource
	t.Cleanup(func() { timeSource = originalTimeSource })
	timeSource = func() time.Time { return testFixedTime }
}

// newFakeSource creates a Source backed by the fake clientset preloaded with objects.
func newFakeSource(t *testing.T, objects ...runtime.Object) *Source {
	t.Helper()
	return &Source{
		apiServer:   testAPIServer,
		clusterName: testClusterName,
		clientset:   fake.NewClientset(objects...),
	}
}

// newHTTPSource creates a Source whose real clientset talks to an httptest server,
// so that query parameters such as the pagination continue token are observable.
func newHTTPSource(t *testing.T, handler http.Handler) *Source {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	clientset, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
	require.NoError(t, err)

	return &Source{
		apiServer:   testAPIServer,
		clusterName: testClusterName,
		clientset:   clientset,
	}
}

// writeJSON writes body as a JSON response.
func writeJSON(t *testing.T, w http.ResponseWriter, body any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(body))
}

func newNode(name string) corev1.Node {
	return corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name}}
}

func newNamespace(name string, labels map[string]string) *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}}
}

// collectData drains a closed results channel.
func collectData(results <-chan source.Data) []source.Data {
	var items []source.Data
	for d := range results {
		items = append(items, d)
	}
	return items
}
