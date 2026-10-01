// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package k8s

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/mia-platform/ibdm/internal/source"
)

func testPodSpec(containers, initContainers []corev1.Container) corev1.PodSpec {
	return corev1.PodSpec{Containers: containers, InitContainers: initContainers}
}

func testContainer(name, image string) corev1.Container {
	return corev1.Container{Name: name, Image: image}
}

func testMeta(namespace, name string, labels map[string]string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Namespace: namespace, Name: name, Labels: labels}
}

func newDeployment(namespace, name string, labels map[string]string, replicas *int32, pod corev1.PodSpec) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: testMeta(namespace, name, labels),
		Spec:       appsv1.DeploymentSpec{Replicas: replicas, Template: corev1.PodTemplateSpec{Spec: pod}},
	}
}

func newStatefulSet(namespace, name string, labels map[string]string, replicas *int32, pod corev1.PodSpec) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: testMeta(namespace, name, labels),
		Spec:       appsv1.StatefulSetSpec{Replicas: replicas, Template: corev1.PodTemplateSpec{Spec: pod}},
	}
}

func newDaemonSet(namespace, name string, labels map[string]string, pod corev1.PodSpec) *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		ObjectMeta: testMeta(namespace, name, labels),
		Spec:       appsv1.DaemonSetSpec{Template: corev1.PodTemplateSpec{Spec: pod}},
	}
}

func int32Ptr(v int32) *int32 { return &v }

// workloadKind describes one workload kind for table-driven tests.
type workloadKind struct {
	dataType    string
	hasReplicas bool
	sync        func(s *Source, ctx context.Context, results chan<- source.Data) error
	path        string
	// build creates an object of this kind.
	build func(namespace, name string, labels map[string]string, replicas *int32, pod corev1.PodSpec) runtime.Object
	// list creates a list response of this kind holding the given objects.
	list func(continueToken string, objects ...runtime.Object) any
}

func workloadKinds() map[string]workloadKind {
	return map[string]workloadKind{
		"deployment": {
			dataType:    deploymentType,
			hasReplicas: true,
			sync:        (*Source).syncDeployments,
			path:        "/apis/apps/v1/deployments",
			build: func(namespace, name string, labels map[string]string, replicas *int32, pod corev1.PodSpec) runtime.Object {
				return newDeployment(namespace, name, labels, replicas, pod)
			},
			list: func(token string, objects ...runtime.Object) any {
				list := appsv1.DeploymentList{ListMeta: metav1.ListMeta{Continue: token}}
				for _, o := range objects {
					list.Items = append(list.Items, *o.(*appsv1.Deployment)) //nolint:forcetypeassert // test helper
				}
				return list
			},
		},
		"statefulset": {
			dataType:    statefulSetType,
			hasReplicas: true,
			sync:        (*Source).syncStatefulSets,
			path:        "/apis/apps/v1/statefulsets",
			build: func(namespace, name string, labels map[string]string, replicas *int32, pod corev1.PodSpec) runtime.Object {
				return newStatefulSet(namespace, name, labels, replicas, pod)
			},
			list: func(token string, objects ...runtime.Object) any {
				list := appsv1.StatefulSetList{ListMeta: metav1.ListMeta{Continue: token}}
				for _, o := range objects {
					list.Items = append(list.Items, *o.(*appsv1.StatefulSet)) //nolint:forcetypeassert // test helper
				}
				return list
			},
		},
		"daemonset": {
			dataType: daemonSetType,
			sync:     (*Source).syncDaemonSets,
			path:     "/apis/apps/v1/daemonsets",
			build: func(namespace, name string, labels map[string]string, _ *int32, pod corev1.PodSpec) runtime.Object {
				return newDaemonSet(namespace, name, labels, pod)
			},
			list: func(token string, objects ...runtime.Object) any {
				list := appsv1.DaemonSetList{ListMeta: metav1.ListMeta{Continue: token}}
				for _, o := range objects {
					list.Items = append(list.Items, *o.(*appsv1.DaemonSet)) //nolint:forcetypeassert // test helper
				}
				return list
			},
		},
	}
}

func TestSyncWorkloads(t *testing.T) {
	setupFixedTime(t)

	for kindName, kind := range workloadKinds() {
		t.Run(kindName, func(t *testing.T) {
			pod := testPodSpec(
				[]corev1.Container{testContainer("web", "nginx:1.27"), testContainer("sidecar", "proxy:2")},
				[]corev1.Container{testContainer("init-b", "busybox:1"), testContainer("init-a", "busybox:2")},
			)
			s := newFakeSource(t,
				kind.build("team-a", "app", map[string]string{"env": "prod"}, int32Ptr(3), pod),
				kind.build("team-b", "app", nil, nil, testPodSpec(nil, nil)),
			)
			results := make(chan source.Data, 5)

			require.NoError(t, kind.sync(s, t.Context(), results))
			close(results)

			items := collectData(results)
			require.Len(t, items, 2)
			for _, item := range items {
				assert.Equal(t, kind.dataType, item.Type)
				assert.Equal(t, source.DataOperationUpsert, item.Operation)
				assert.Equal(t, testFixedTime, item.Time)
				assert.Equal(t, testAPIServer, item.Values["apiServer"])
			}

			byNamespace := map[any]map[string]any{}
			for _, item := range items {
				byNamespace[item.Values["namespace"]] = item.Values
			}
			require.Contains(t, byNamespace, "team-a")
			require.Contains(t, byNamespace, "team-b")

			full := byNamespace["team-a"]
			assert.Equal(t, "app", full["name"])
			assert.Equal(t, map[string]string{"env": "prod"}, full["labels"])
			assert.Equal(t, []map[string]any{
				{"name": "web", "image": "nginx:1.27"},
				{"name": "sidecar", "image": "proxy:2"},
			}, full["containers"])
			assert.Equal(t, []map[string]any{
				{"name": "init-b", "image": "busybox:1"},
				{"name": "init-a", "image": "busybox:2"},
			}, full["initContainers"])

			empty := byNamespace["team-b"]
			assert.Equal(t, map[string]string{}, empty["labels"])
			assert.Equal(t, []map[string]any{}, empty["containers"])
			assert.Equal(t, []map[string]any{}, empty["initContainers"])

			if kind.hasReplicas {
				assert.Equal(t, 3, full["replicas"])
				assert.Equal(t, 1, empty["replicas"])
			} else {
				assert.NotContains(t, full, "replicas")
				assert.NotContains(t, empty, "replicas")
			}
		})
	}
}

func TestSyncWorkloadsEmpty(t *testing.T) {
	t.Parallel()

	for kindName, kind := range workloadKinds() {
		t.Run(kindName, func(t *testing.T) {
			t.Parallel()

			results := make(chan source.Data, 1)
			require.NoError(t, kind.sync(newFakeSource(t), t.Context(), results))
			assert.Empty(t, results)
		})
	}
}

func TestSyncWorkloadsPagination(t *testing.T) {
	setupFixedTime(t)

	for kindName, kind := range workloadKinds() {
		t.Run(kindName, func(t *testing.T) {
			var limits []string
			s := newHTTPSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, kind.path, r.URL.Path)
				limits = append(limits, r.URL.Query().Get("limit"))
				if r.URL.Query().Get("continue") == "" {
					writeJSON(t, w, kind.list("page-2", kind.build("team-a", "one", nil, nil, testPodSpec(nil, nil))))
					return
				}
				assert.Equal(t, "page-2", r.URL.Query().Get("continue"))
				writeJSON(t, w, kind.list("", kind.build("team-b", "two", nil, nil, testPodSpec(nil, nil))))
			}))
			results := make(chan source.Data, 5)

			require.NoError(t, kind.sync(s, t.Context(), results))
			close(results)

			assert.Len(t, collectData(results), 2)
			assert.Equal(t, []string{"500", "500"}, limits)
		})
	}
}

func TestSyncWorkloadsListError(t *testing.T) {
	t.Parallel()

	for kindName, kind := range workloadKinds() {
		t.Run(kindName, func(t *testing.T) {
			t.Parallel()

			s := newHTTPSource(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusForbidden)
			}))
			require.ErrorIs(t, kind.sync(s, t.Context(), make(chan source.Data, 1)), ErrRetrievingAssets)
		})
	}
}

func TestSyncWorkloadsContextCanceled(t *testing.T) {
	t.Parallel()

	for kindName, kind := range workloadKinds() {
		t.Run(kindName, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			cancel()

			s := newFakeSource(t, kind.build("team-a", "app", nil, nil, testPodSpec(nil, nil)))
			require.ErrorIs(t, kind.sync(s, ctx, make(chan source.Data)), context.Canceled)
		})
	}
}

func TestSyncWorkloadsCanceledWhileSending(t *testing.T) {
	t.Parallel()

	for kindName, kind := range workloadKinds() {
		t.Run(kindName, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			s := newFakeSource(t, kind.build("team-a", "app", nil, nil, testPodSpec(nil, nil)))

			// unbuffered channel with no reader: the send blocks until the context is canceled.
			done := make(chan error, 1)
			go func() { done <- kind.sync(s, ctx, make(chan source.Data)) }()
			cancel()

			require.ErrorIs(t, <-done, context.Canceled)
		})
	}
}

func TestStartSyncProcessWorkloadsDispatch(t *testing.T) {
	setupFixedTime(t)

	pod := testPodSpec(nil, nil)
	s := newFakeSource(t,
		newDeployment("team-a", "web", nil, nil, pod),
		newStatefulSet("team-a", "db", nil, nil, pod),
		newDaemonSet("team-a", "agent", nil, pod),
	)
	results := make(chan source.Data, 10)

	types := map[string]source.Extra{deploymentType: {}, statefulSetType: {}, daemonSetType: {}, "unknown": {}}
	require.NoError(t, s.StartSyncProcess(t.Context(), types, results))
	close(results)

	items := collectData(results)
	gotTypes := make([]string, 0, len(items))
	for _, item := range items {
		gotTypes = append(gotTypes, item.Type)
	}
	assert.Equal(t, []string{deploymentType, statefulSetType, daemonSetType}, gotTypes)
}

func TestStartSyncProcessWorkloadsErrorIsolation(t *testing.T) {
	setupFixedTime(t)

	// statefulsets are forbidden, the other workloads are served.
	s := newHTTPSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		kinds := workloadKinds()
		switch r.URL.Path {
		case kinds["deployment"].path:
			writeJSON(t, w, kinds["deployment"].list("", newDeployment("team-a", "web", nil, nil, testPodSpec(nil, nil))))
		case kinds["daemonset"].path:
			writeJSON(t, w, kinds["daemonset"].list("", newDaemonSet("team-a", "agent", nil, testPodSpec(nil, nil))))
		default:
			w.WriteHeader(http.StatusForbidden)
		}
	}))
	results := make(chan source.Data, 10)

	types := map[string]source.Extra{deploymentType: {}, statefulSetType: {}, daemonSetType: {}}
	err := s.StartSyncProcess(t.Context(), types, results)
	require.ErrorIs(t, err, ErrK8sSource)
	require.ErrorIs(t, err, ErrRetrievingAssets)
	close(results)

	items := collectData(results)
	require.Len(t, items, 2)
	assert.Equal(t, deploymentType, items[0].Type)
	assert.Equal(t, daemonSetType, items[1].Type)
}
