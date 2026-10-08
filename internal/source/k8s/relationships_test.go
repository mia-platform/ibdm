// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package k8s

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"

	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/mia-platform/ibdm/internal/source"
)

// routeServices builds the spec of an IngressRoute with one route per services list.
func routeServices(routes ...[]any) map[string]any {
	items := make([]any, 0, len(routes))
	for _, services := range routes {
		items = append(items, map[string]any{"match": "Host(`a.example.com`)", "services": services})
	}
	return map[string]any{"routes": items}
}

func svcRef(name, namespace, kind string) map[string]any {
	ref := map[string]any{"name": name}
	if namespace != "" {
		ref["namespace"] = namespace
	}
	if kind != "" {
		ref["kind"] = kind
	}
	return ref
}

func ingressPair(routeNamespace, route, serviceNamespace, service string) map[string]any {
	return map[string]any{
		keyAPIServer:        testAPIServer,
		keyNamespace:        routeNamespace,
		keyName:             route,
		keyServiceNamespace: serviceNamespace,
		keyServiceName:      service,
	}
}

func releasePair(kind, namespace, name, releaseNamespace, release string) map[string]any {
	return map[string]any{
		keyAPIServer:        testAPIServer,
		keyKind:             kind,
		keyNamespace:        namespace,
		keyName:             name,
		keyReleaseName:      release,
		keyReleaseNamespace: releaseNamespace,
	}
}

func pairValues(pairs []relation) []map[string]any {
	if len(pairs) == 0 {
		return nil
	}
	values := make([]map[string]any, 0, len(pairs))
	for _, pair := range pairs {
		values = append(values, pair.values)
	}
	return values
}

func helmAnnotations(release, namespace string) map[string]string {
	return map[string]string{helmReleaseNameAnnotation: release, helmReleaseNamespaceAnnotation: namespace}
}

func TestIngressRouteServicePairs(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		spec map[string]any
		want []map[string]any
	}{
		"no spec": {},
		"service kinds": {
			spec: routeServices([]any{
				svcRef("a", "", ""), svcRef("b", "", "Service"), svcRef("c", "", "TraefikService"), svcRef("d", "", "Other"),
			}),
			want: []map[string]any{ingressPair("team-a", "web", "team-a", "a"), ingressPair("team-a", "web", "team-a", "b")},
		},
		"namespace defaulting": {
			spec: routeServices([]any{svcRef("a", "team-b", ""), svcRef("b", "", "")}),
			want: []map[string]any{ingressPair("team-a", "web", "team-b", "a"), ingressPair("team-a", "web", "team-a", "b")},
		},
		"deduplication across routes and entries": {
			spec: routeServices(
				[]any{svcRef("a", "", ""), svcRef("a", "team-a", "Service"), svcRef("b", "", "")},
				[]any{svcRef("a", "", ""), svcRef("a", "team-b", "")},
			),
			want: []map[string]any{
				ingressPair("team-a", "web", "team-a", "a"),
				ingressPair("team-a", "web", "team-a", "b"),
				ingressPair("team-a", "web", "team-b", "a"),
			},
		},
		"missing and odd fields": {
			spec: map[string]any{"routes": []any{
				"not a map",
				map[string]any{"services": "not a list"},
				map[string]any{"services": []any{map[string]any{"namespace": "team-b"}, 3, map[string]any{"name": 4}, svcRef("ok", "", "")}},
				map[string]any{},
			}},
			want: []map[string]any{ingressPair("team-a", "web", "team-a", "ok")},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			route := newIngressRouteObject("team-a", "web", nil, test.spec)
			assert.Equal(t, test.want, pairValues(ingressRouteServicePairs(route.Object, testAPIServer)))
		})
	}
}

func TestWorkloadReleasePairs(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		annotations map[string]string
		want        []map[string]any
	}{
		"both annotations": {
			annotations: helmAnnotations("shop", "team-c"),
			want:        []map[string]any{releasePair("Deployment", "team-a", "web", "team-c", "shop")},
		},
		"only the release name": {annotations: map[string]string{helmReleaseNameAnnotation: "shop"}},
		"only the namespace":    {annotations: map[string]string{helmReleaseNamespaceAnnotation: "team-c"}},
		"empty values":          {annotations: helmAnnotations("", "")},
		"no annotations":        {},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			meta := metav1.ObjectMeta{Namespace: "team-a", Name: "web", Annotations: test.annotations}
			assert.Equal(t, test.want, pairValues(workloadReleasePairs("Deployment", &meta, testAPIServer)))
		})
	}
}

func TestRelationshipPairsIgnoreUnexpectedObjects(t *testing.T) {
	t.Parallel()

	for _, binding := range relationshipBindings {
		assert.Empty(t, binding.pairs("not an object", testAPIServer), binding.sourceType)
		assert.Empty(t, binding.pairs((*appsv1.Deployment)(nil), testAPIServer), binding.sourceType)
		assert.Empty(t, binding.pairs((*unstructured.Unstructured)(nil), testAPIServer), binding.sourceType)
	}
}

func TestRelationshipTypesAreKnownAndStreamable(t *testing.T) {
	t.Parallel()

	for _, name := range []string{ingressRouteServiceRelationshipType, workloadHelmReleaseRelationshipType} {
		assert.True(t, isStreamable(name), name)
		assert.True(t, isKnownType(name), name)
	}
	assert.False(t, isStreamable("unknownRelationship"))

	last := knownTypes[len(knownTypes)-4 : len(knownTypes)-1]
	assert.Equal(t, ingressRouteServiceRelationshipType, last[0].name)
	assert.Equal(t, workloadHelmReleaseRelationshipType, last[1].name)
	assert.Equal(t, serviceWorkloadRelationshipType, last[2].name)
	assert.Equal(t, podType, knownTypes[len(knownTypes)-5].name)
}

func TestSyncIngressRouteServiceRelationships(t *testing.T) {
	setupFixedTime(t)

	routeA := newIngressRouteObject("team-a", "web", nil, routeServices(
		[]any{svcRef("api", "team-b", ""), svcRef("ui", "", "Service"), svcRef("lb", "", "TraefikService")},
		[]any{svcRef("ui", "", "")},
	))
	routeB := newIngressRouteObject("team-b", "admin", nil, routeServices([]any{svcRef("ghost", "", "")}))
	noServices := newIngressRouteObject("team-b", "empty", nil, nil)

	s := newCRDSource(t, allServed(), routeA, routeB, noServices)
	items, err := syncOne(t, (*Source).syncIngressRouteServiceRelationships, s)
	require.NoError(t, err)

	got := make([]map[string]any, 0, len(items))
	for _, item := range items {
		assert.Equal(t, ingressRouteServiceRelationshipType, item.Type)
		assert.Equal(t, source.DataOperationUpsert, item.Operation)
		assert.Equal(t, testFixedTime, item.Time)
		got = append(got, item.Values)
	}
	assert.ElementsMatch(t, []map[string]any{
		ingressPair("team-a", "web", "team-b", "api"),
		ingressPair("team-a", "web", "team-a", "ui"),
		ingressPair("team-b", "admin", "team-b", "ghost"),
	}, got)
}

func TestSyncIngressRouteServiceRelationshipsNotInstalled(t *testing.T) {
	setupFixedTime(t)

	s := newCRDSource(t, nil)
	items, err := syncOne(t, (*Source).syncIngressRouteServiceRelationships, s)
	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestSyncIngressRouteServiceRelationshipsErrors(t *testing.T) {
	t.Parallel()

	t.Run("list error", func(t *testing.T) {
		t.Parallel()
		s := newCRDSource(t, allServed())
		fakeDyn, ok := s.dynamic.(*dynamicfake.FakeDynamicClient)
		require.True(t, ok)
		fakeDyn.PrependReactor("list", ingressRoutesRes, func(k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("forbidden")
		})
		_, err := syncOne(t, (*Source).syncIngressRouteServiceRelationships, s)
		require.ErrorIs(t, err, ErrRetrievingAssets)
	})

	t.Run("canceled while sending", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(t.Context())
		s := newCRDSource(t, allServed(), newIngressRouteObject("team-a", "web", nil, routeServices([]any{svcRef("api", "", "")})))

		done := make(chan error, 1)
		go func() { done <- s.syncIngressRouteServiceRelationships(ctx, make(chan source.Data)) }()
		cancel()
		require.ErrorIs(t, <-done, context.Canceled)
	})
}

func TestSyncWorkloadHelmReleaseRelationships(t *testing.T) {
	setupFixedTime(t)

	pod := testPodSpec(nil, nil)
	dep := newDeployment("team-a", "web", nil, nil, pod)
	dep.Annotations = helmAnnotations("shop", "team-c")
	partial := newDeployment("team-a", "partial", nil, nil, pod)
	partial.Annotations = map[string]string{helmReleaseNameAnnotation: "shop"}
	plain := newDeployment("team-a", "plain", nil, nil, pod)
	sts := newStatefulSet("team-a", "db", nil, nil, pod)
	sts.Annotations = helmAnnotations("data", "team-d")
	ds := newDaemonSet("team-b", "agent", nil, pod)
	ds.Annotations = helmAnnotations("shop", "team-c")

	s := newFakeSource(t, dep, partial, plain, sts, ds)
	items, err := syncOne(t, (*Source).syncWorkloadHelmReleaseRelationships, s)
	require.NoError(t, err)

	got := make([]map[string]any, 0, len(items))
	for _, item := range items {
		assert.Equal(t, workloadHelmReleaseRelationshipType, item.Type)
		assert.Equal(t, source.DataOperationUpsert, item.Operation)
		assert.Equal(t, testFixedTime, item.Time)
		got = append(got, item.Values)
	}
	assert.ElementsMatch(t, []map[string]any{
		releasePair("Deployment", "team-a", "web", "team-c", "shop"),
		releasePair("StatefulSet", "team-a", "db", "team-d", "data"),
		releasePair("DaemonSet", "team-b", "agent", "team-c", "shop"),
	}, got)
}

func TestSyncWorkloadHelmReleaseRelationshipsErrors(t *testing.T) {
	setupFixedTime(t)

	t.Run("one kind failing does not stop the others", func(t *testing.T) {
		dep := newDeployment("team-a", "web", nil, nil, testPodSpec(nil, nil))
		dep.Annotations = helmAnnotations("shop", "team-c")
		s := newFakeSource(t, dep)
		fakeClient, ok := s.clientset.(*fake.Clientset)
		require.True(t, ok)
		fakeClient.PrependReactor("list", "statefulsets", func(k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("forbidden")
		})

		items, err := syncOne(t, (*Source).syncWorkloadHelmReleaseRelationships, s)
		require.ErrorIs(t, err, ErrRetrievingAssets)
		require.Len(t, items, 1)
		assert.Equal(t, "web", items[0].Values[keyName])
	})

	t.Run("canceled while sending", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		dep := newDeployment("team-a", "web", nil, nil, testPodSpec(nil, nil))
		dep.Annotations = helmAnnotations("shop", "team-c")
		s := newFakeSource(t, dep)

		done := make(chan error, 1)
		go func() { done <- s.syncWorkloadHelmReleaseRelationships(ctx, make(chan source.Data)) }()
		cancel()
		require.ErrorIs(t, <-done, context.Canceled)
	})
}

func TestStartSyncProcessRelationships(t *testing.T) {
	setupFixedTime(t)

	route := newIngressRouteObject("team-a", "web", nil, routeServices([]any{svcRef("api", "", "")}))
	dep := newDeployment("team-a", "web", nil, nil, testPodSpec(nil, nil))
	dep.Annotations = helmAnnotations("shop", "team-c")

	s := newCRDSource(t, allServed(), route)
	fakeClient, ok := s.clientset.(*fake.Clientset)
	require.True(t, ok)
	require.NoError(t, fakeClient.Tracker().Add(dep))
	results := make(chan source.Data, 10)

	types := map[string]source.Extra{workloadHelmReleaseRelationshipType: {}, ingressRouteServiceRelationshipType: {}, namespaceType: {}}
	require.NoError(t, s.StartSyncProcess(t.Context(), types, results))
	close(results)

	got := make([]string, 0, 2)
	for _, item := range collectData(results) {
		got = append(got, item.Type)
	}
	assert.Equal(t, []string{ingressRouteServiceRelationshipType, workloadHelmReleaseRelationshipType}, got)
}

// relationStream starts the event stream for types with a fake discovery serving the CRDs.
func relationStream(t *testing.T, types []string, crdObjects []runtime.Object) *streamHarness {
	t.Helper()
	return startStreamWithCRDs(t, types, serving(allServed()...), crdObjects)
}

// routes returns the dynamic client of the IngressRoutes of namespace team-a.
func (h *streamHarness) routes() dynamic.ResourceInterface {
	return h.dynamic.Resource(traefikV1Alpha1).Namespace("team-a")
}

// upsertOf and deleteOf build the expected data of a relationship event.
func relationData(dataType string, operation source.DataOperation, values map[string]any) source.Data {
	return source.Data{Type: dataType, Operation: operation, Values: values, Time: testFixedTime}
}

func TestStartEventStreamIngressRouteRelationships(t *testing.T) {
	const relType = ingressRouteServiceRelationshipType
	route := newIngressRouteObject("team-a", "web", nil, routeServices(
		[]any{svcRef("api", "team-b", ""), svcRef("ui", "", "Service"), svcRef("lb", "", "TraefikService")},
		[]any{svcRef("ui", "", "")},
	))

	t.Run("snapshot without the item type", func(t *testing.T) {
		harness := relationStream(t, []string{relType}, []runtime.Object{route})

		assert.ElementsMatch(t, []source.Data{
			relationData(relType, source.DataOperationUpsert, ingressPair("team-a", "web", "team-b", "api")),
			relationData(relType, source.DataOperationUpsert, ingressPair("team-a", "web", "team-a", "ui")),
		}, harness.nextN(2))
		harness.waitWatches(1)
		assert.Empty(t, harness.results, "the ingressroute item type was not requested")
	})

	t.Run("update adds and removes services", func(t *testing.T) {
		harness := relationStream(t, []string{relType}, []runtime.Object{route})
		harness.nextN(2)
		harness.waitWatches(1)

		updated := newIngressRouteObject("team-a", "web", nil, routeServices(
			[]any{svcRef("ui", "", ""), svcRef("cache", "", "")},
		))
		_, err := harness.routes().Update(t.Context(), updated, metav1.UpdateOptions{})
		require.NoError(t, err)

		assert.Equal(t, []source.Data{
			relationData(relType, source.DataOperationUpsert, ingressPair("team-a", "web", "team-a", "cache")),
			relationData(relType, source.DataOperationDelete, ingressPair("team-a", "web", "team-b", "api")),
		}, harness.nextN(2))
	})

	t.Run("unchanged pairs emit nothing", func(t *testing.T) {
		harness := relationStream(t, []string{relType}, []runtime.Object{route})
		harness.nextN(2)
		harness.waitWatches(1)

		// same pairs: a new match, a duplicated entry, a TraefikService and an annotation.
		same := newIngressRouteObject("team-a", "web", map[string]any{"v": "2"}, routeServices(
			[]any{svcRef("ui", "", ""), svcRef("api", "team-b", "Service"), svcRef("other", "", "TraefikService")},
			[]any{svcRef("ui", "", "")},
		))
		setAnnotation(same, "note")
		_, err := harness.routes().Update(t.Context(), same, metav1.UpdateOptions{})
		require.NoError(t, err)

		// the next item is the real change: nothing was emitted for the previous update.
		changed := newIngressRouteObject("team-a", "web", nil, routeServices([]any{svcRef("ui", "", "")}))
		_, err = harness.routes().Update(t.Context(), changed, metav1.UpdateOptions{})
		require.NoError(t, err)
		assert.Equal(t, []source.Data{
			relationData(relType, source.DataOperationDelete, ingressPair("team-a", "web", "team-b", "api")),
		}, harness.nextN(1))
		assert.Empty(t, harness.results)
	})

	t.Run("create and delete of the route", func(t *testing.T) {
		harness := relationStream(t, []string{relType}, nil)
		harness.waitWatches(1)

		_, err := harness.routes().Create(t.Context(), route, metav1.CreateOptions{})
		require.NoError(t, err)
		assert.ElementsMatch(t, []source.Data{
			relationData(relType, source.DataOperationUpsert, ingressPair("team-a", "web", "team-b", "api")),
			relationData(relType, source.DataOperationUpsert, ingressPair("team-a", "web", "team-a", "ui")),
		}, harness.nextN(2))

		require.NoError(t, harness.routes().Delete(t.Context(), "web", metav1.DeleteOptions{}))
		assert.ElementsMatch(t, []source.Data{
			relationData(relType, source.DataOperationDelete, ingressPair("team-a", "web", "team-b", "api")),
			relationData(relType, source.DataOperationDelete, ingressPair("team-a", "web", "team-a", "ui")),
		}, harness.nextN(2))
	})

	t.Run("item and relationship types share one informer", func(t *testing.T) {
		harness := relationStream(t, []string{ingressRouteType, relType}, []runtime.Object{route})

		var items, relations int
		for _, data := range harness.nextN(3) {
			switch data.Type {
			case ingressRouteType:
				items++
			case relType:
				relations++
			}
		}
		assert.Equal(t, 1, items)
		assert.Equal(t, 2, relations)
		harness.waitWatches(1)

		var lists, watches int
		for _, action := range harness.dynamic.Actions() {
			switch action.GetVerb() {
			case "list":
				lists++
			case "watch":
				watches++
			}
		}
		assert.Equal(t, 1, lists)
		assert.Equal(t, 1, watches)
	})

	t.Run("CRD not installed", func(t *testing.T) {
		s := newCRDSource(t, nil)
		results := make(chan source.Data, 1)
		require.NoError(t, s.StartEventStream(t.Context(), map[string]source.Extra{relType: nil}, results))
		assert.Empty(t, results)
	})
}

func TestStartEventStreamWorkloadRelationships(t *testing.T) {
	const relType = workloadHelmReleaseRelationshipType
	pod := testPodSpec(nil, nil)

	annotated := func(obj metav1.Object, annotations map[string]string) {
		obj.SetAnnotations(annotations)
	}
	dep := newDeployment("team-a", "web", nil, nil, pod)
	annotated(dep, helmAnnotations("shop", "team-c"))
	sts := newStatefulSet("team-a", "db", nil, nil, pod)
	annotated(sts, helmAnnotations("data", "team-d"))
	ds := newDaemonSet("team-b", "agent", nil, pod)
	annotated(ds, helmAnnotations("shop", "team-c"))
	partial := newDeployment("team-a", "partial", nil, nil, pod)
	annotated(partial, map[string]string{helmReleaseNameAnnotation: "shop"})
	plain := newDeployment("team-a", "plain", nil, nil, pod)

	t.Run("snapshot without the item types", func(t *testing.T) {
		harness := startStream(t, []string{relType}, dep, sts, ds, partial, plain)

		assert.ElementsMatch(t, []source.Data{
			relationData(relType, source.DataOperationUpsert, releasePair("Deployment", "team-a", "web", "team-c", "shop")),
			relationData(relType, source.DataOperationUpsert, releasePair("StatefulSet", "team-a", "db", "team-d", "data")),
			relationData(relType, source.DataOperationUpsert, releasePair("DaemonSet", "team-b", "agent", "team-c", "shop")),
		}, harness.nextN(3))
		harness.waitWatches(3)
		assert.Empty(t, harness.results)
	})

	t.Run("annotation changes", func(t *testing.T) {
		harness := startStream(t, []string{relType}, versioned(dep.DeepCopy(), "1"))
		harness.nextN(1)
		harness.waitWatches(3)

		withRelease := func(annotations map[string]string, labels map[string]string, version string) *appsv1.Deployment {
			obj := newDeployment("team-a", "web", labels, nil, pod)
			obj.Annotations = annotations
			return versioned(obj, version)
		}

		// release changed: the new link is upserted and the old one deleted.
		harness.update(deploymentsGVR, withRelease(helmAnnotations("shop-v2", "team-c"), nil, "2"), "team-a")
		assert.Equal(t, []source.Data{
			relationData(relType, source.DataOperationUpsert, releasePair("Deployment", "team-a", "web", "team-c", "shop-v2")),
			relationData(relType, source.DataOperationDelete, releasePair("Deployment", "team-a", "web", "team-c", "shop")),
		}, harness.nextN(2))

		// unrelated change: nothing is emitted.
		harness.update(deploymentsGVR, withRelease(helmAnnotations("shop-v2", "team-c"), map[string]string{"x": "y"}, "3"), "team-a")

		// annotations removed: the link is deleted.
		harness.update(deploymentsGVR, withRelease(nil, nil, "4"), "team-a")
		assert.Equal(t, []source.Data{
			relationData(relType, source.DataOperationDelete, releasePair("Deployment", "team-a", "web", "team-c", "shop-v2")),
		}, harness.nextN(1))

		// annotations added again: the link is upserted.
		harness.update(deploymentsGVR, withRelease(helmAnnotations("shop", "team-c"), nil, "5"), "team-a")
		assert.Equal(t, []source.Data{
			relationData(relType, source.DataOperationUpsert, releasePair("Deployment", "team-a", "web", "team-c", "shop")),
		}, harness.nextN(1))
		assert.Empty(t, harness.results)
	})

	t.Run("delete of the workloads", func(t *testing.T) {
		harness := startStream(t, []string{relType}, dep, sts, ds)
		harness.nextN(3)
		harness.waitWatches(3)

		harness.remove(deploymentsGVR, "team-a", "web")
		harness.remove(statefulSetsGVR, "team-a", "db")
		harness.remove(daemonSetsGVR, "team-b", "agent")

		got := harness.nextN(3)
		for _, data := range got {
			assert.Equal(t, source.DataOperationDelete, data.Operation)
		}
		assert.ElementsMatch(t, []map[string]any{
			releasePair("Deployment", "team-a", "web", "team-c", "shop"),
			releasePair("StatefulSet", "team-a", "db", "team-d", "data"),
			releasePair("DaemonSet", "team-b", "agent", "team-c", "shop"),
		}, []map[string]any{got[0].Values, got[1].Values, got[2].Values})
	})

	t.Run("item and relationship types share one informer", func(t *testing.T) {
		harness := startStream(t, []string{deploymentType, relType}, dep)

		types := map[string]int{}
		for _, data := range harness.nextN(2) {
			types[data.Type]++
		}
		assert.Equal(t, map[string]int{deploymentType: 1, relType: 1}, types)
		harness.waitWatches(1)

		var lists, watches int
		for _, action := range harness.client.Actions() {
			if action.GetResource().Resource != "deployments" {
				continue
			}
			switch action.GetVerb() {
			case "list":
				lists++
			case "watch":
				watches++
			}
		}
		assert.Equal(t, 1, lists)
		assert.Equal(t, 1, watches)
	})
}

func TestRelationHandlersTombstoneAndUnexpectedObjects(t *testing.T) {
	setupFixedTime(t)
	results := make(chan source.Data, 10)
	stream := &eventStream{ctx: t.Context(), log: nilLogger(t), apiServer: testAPIServer, results: results}
	handler := stream.relationHandlers(relationshipBindings[1])

	dep := newDeployment("team-a", "web", nil, nil, testPodSpec(nil, nil))
	dep.Annotations = helmAnnotations("shop", "team-c")

	handler.OnDelete(cache.DeletedFinalStateUnknown{Key: "team-a/web", Obj: dep})
	require.Len(t, results, 1)
	assert.Equal(t, relationData(workloadHelmReleaseRelationshipType, source.DataOperationDelete, releasePair("Deployment", "team-a", "web", "team-c", "shop")), <-results)

	handler.OnDelete(cache.DeletedFinalStateUnknown{Key: "x", Obj: "unexpected"})
	handler.OnAdd("unexpected", false)
	handler.OnUpdate("unexpected", "unexpected")
	assert.Empty(t, results)

	// same non empty resource version: skipped without comparing.
	handler.OnUpdate(versioned(dep.DeepCopy(), "1"), versioned(newDeployment("team-a", "web", nil, nil, testPodSpec(nil, nil)), "1"))
	assert.Empty(t, results)
}

func TestRegisterSetsUpEveryHandler(t *testing.T) {
	setupFixedTime(t)
	results := make(chan source.Data, 10)
	stream := &eventStream{ctx: t.Context(), log: nilLogger(t), apiServer: testAPIServer, results: results}

	kind := findEventKind(t, deploymentType)
	handlers := stream.sourceHandlers(kind, map[string]struct{}{deploymentType: {}, workloadHelmReleaseRelationshipType: {}, ingressRouteServiceRelationshipType: {}})
	assert.Len(t, handlers, 2)
	assert.Empty(t, stream.sourceHandlers(kind, map[string]struct{}{namespaceType: {}}))
	assert.Len(t, stream.sourceHandlers(kind, map[string]struct{}{workloadHelmReleaseRelationshipType: {}}), 1)
}

const swType = serviceWorkloadRelationshipType

func selecting(namespace, name string, selector map[string]string) *corev1.Service {
	return newService(namespace, name, nil, corev1.ServiceSpec{Selector: selector})
}

func labelledDeployment(namespace, name string, labels map[string]string) *appsv1.Deployment {
	dep := newDeployment(namespace, name, nil, nil, testPodSpec(nil, nil))
	dep.Spec.Template.Labels = labels
	return dep
}

func labelledStatefulSet(namespace, name string, labels map[string]string) *appsv1.StatefulSet {
	sts := newStatefulSet(namespace, name, nil, nil, testPodSpec(nil, nil))
	sts.Spec.Template.Labels = labels
	return sts
}

func labelledDaemonSet(namespace, name string, labels map[string]string) *appsv1.DaemonSet {
	ds := newDaemonSet(namespace, name, nil, testPodSpec(nil, nil))
	ds.Spec.Template.Labels = labels
	return ds
}

func swPair(namespace, service, kind, workload string) map[string]any {
	return map[string]any{
		keyAPIServer:    testAPIServer,
		keyNamespace:    namespace,
		keyServiceName:  service,
		keyWorkloadKind: kind,
		keyWorkloadName: workload,
	}
}

func swData(operation source.DataOperation, namespace, service, kind, workload string) source.Data {
	return relationData(swType, operation, swPair(namespace, service, kind, workload))
}

func lbl(pairs ...string) map[string]string {
	result := make(map[string]string, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		result[pairs[i]] = pairs[i+1]
	}
	return result
}

func TestSelectorMatches(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		selector map[string]string
		labels   map[string]string
		want     bool
	}{
		"exact match":         {selector: lbl("app", "web"), labels: lbl("app", "web"), want: true},
		"subset of labels":    {selector: lbl("app", "web"), labels: lbl("app", "web", "tier", "front", "v", "1"), want: true},
		"all pairs required":  {selector: lbl("app", "web", "tier", "front"), labels: lbl("app", "web", "tier", "front"), want: true},
		"missing label":       {selector: lbl("app", "web", "tier", "front"), labels: lbl("app", "web")},
		"different value":     {selector: lbl("app", "web"), labels: lbl("app", "api")},
		"empty value matters": {selector: lbl("app", ""), labels: lbl("app", "web")},
		"empty value equal":   {selector: lbl("app", ""), labels: lbl("app", ""), want: true},
		"empty selector":      {selector: lbl(), labels: lbl("app", "web")},
		"nil selector":        {labels: lbl("app", "web")},
		"nil template labels": {selector: lbl("app", "web")},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, test.want, selectorMatches(test.selector, test.labels))
		})
	}
}

func TestServiceWorkloadPairs(t *testing.T) {
	t.Parallel()

	web := lbl("app", "web")
	tests := map[string]struct {
		services  []serviceRef
		workloads []workloadRef
		want      []map[string]any
	}{
		"no services":               {workloads: []workloadRef{{kind: kindDeployment, name: "web", templateLabels: web}}},
		"no workloads":              {services: []serviceRef{{name: "web", selector: web}}},
		"selector-less services":    {services: []serviceRef{{name: "ext"}, {name: "headless", selector: lbl()}}, workloads: []workloadRef{{kind: kindDeployment, name: "web", templateLabels: web}, {kind: kindDaemonSet, name: "bare"}}},
		"workload without labels":   {services: []serviceRef{{name: "web", selector: web}}, workloads: []workloadRef{{kind: kindDeployment, name: "web"}}},
		"extra labels on template":  {services: []serviceRef{{name: "web", selector: web}}, workloads: []workloadRef{{kind: kindDeployment, name: "web", templateLabels: lbl("app", "web", "v", "2")}}, want: []map[string]any{swPair("ns", "web", "Deployment", "web")}},
		"missing label":             {services: []serviceRef{{name: "web", selector: lbl("app", "web", "tier", "front")}}, workloads: []workloadRef{{kind: kindDeployment, name: "web", templateLabels: web}}},
		"several workloads, sorted": {services: []serviceRef{{name: "web", selector: web}}, workloads: []workloadRef{{kind: kindStatefulSet, name: "b", templateLabels: web}, {kind: kindDeployment, name: "z", templateLabels: web}, {kind: kindDaemonSet, name: "a", templateLabels: web}, {kind: kindDeployment, name: "c", templateLabels: web}}, want: []map[string]any{swPair("ns", "web", "DaemonSet", "a"), swPair("ns", "web", "Deployment", "c"), swPair("ns", "web", "Deployment", "z"), swPair("ns", "web", "StatefulSet", "b")}},
		"several services, sorted":  {services: []serviceRef{{name: "b", selector: web}, {name: "a", selector: lbl("app", "web")}, {name: "c", selector: lbl("app", "api")}}, workloads: []workloadRef{{kind: kindDeployment, name: "web", templateLabels: web}}, want: []map[string]any{swPair("ns", "a", "Deployment", "web"), swPair("ns", "b", "Deployment", "web")}},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := serviceWorkloadPairs(testAPIServer, "ns", test.services, test.workloads)
			assert.Equal(t, test.want, pairValues(got))
			for _, pair := range got {
				assert.Equal(t, pair.values[keyServiceName].(string)+"/"+pair.values[keyWorkloadKind].(string)+"/"+pair.values[keyWorkloadName].(string), pair.key)
			}
		})
	}
}

func TestServiceWorkloadPairsDoesNotModifyInputs(t *testing.T) {
	t.Parallel()

	services := []serviceRef{{name: "b", selector: lbl("app", "web")}, {name: "a", selector: lbl("app", "web")}}
	workloads := []workloadRef{{kind: kindStatefulSet, name: "z", templateLabels: lbl("app", "web")}, {kind: kindDeployment, name: "a", templateLabels: lbl("app", "web")}}
	serviceWorkloadPairs(testAPIServer, "ns", services, workloads)
	assert.Equal(t, "b", services[0].name)
	assert.Equal(t, "z", workloads[0].name)
}

func TestServiceAndWorkloadRefsIgnoreUnexpectedObjects(t *testing.T) {
	t.Parallel()

	_, ok := serviceRefOf("nope")
	assert.False(t, ok)
	_, ok = serviceRefOf((*corev1.Service)(nil))
	assert.False(t, ok)
	for _, obj := range []any{"nope", (*appsv1.Deployment)(nil), (*appsv1.StatefulSet)(nil), (*appsv1.DaemonSet)(nil)} {
		_, ok = workloadRefOf(obj)
		assert.False(t, ok)
	}

	assert.True(t, workloadLabelsChanged("nope", labelledDeployment("ns", "a", nil)))
	assert.True(t, serviceWorkloadSources[0].changed("nope", selecting("ns", "a", nil)))
}

func TestSyncServiceWorkloadRelationships(t *testing.T) {
	setupFixedTime(t)

	web := lbl("app", "web")
	s := newFakeSource(t,
		selecting("team-b", "api", lbl("app", "api")),
		selecting("team-a", "web", web),
		selecting("team-a", "frontend", lbl("tier", "front")),
		selecting("team-a", "external", nil),
		selecting("team-a", "headless", lbl()),
		selecting("team-c", "lonely", web),
		labelledDeployment("team-a", "web", lbl("app", "web", "tier", "front")),
		labelledStatefulSet("team-a", "web-db", web),
		labelledDaemonSet("team-a", "agent", lbl("tier", "front")),
		labelledDeployment("team-a", "unlabelled", nil),
		labelledDeployment("team-b", "api", lbl("app", "api")),
		labelledDeployment("team-b", "web-elsewhere", web),
	)

	items, err := syncOne(t, (*Source).syncServiceWorkloadRelationships, s)
	require.NoError(t, err)

	want := []source.Data{
		swData(source.DataOperationUpsert, "team-a", "frontend", "DaemonSet", "agent"),
		swData(source.DataOperationUpsert, "team-a", "frontend", "Deployment", "web"),
		swData(source.DataOperationUpsert, "team-a", "web", "Deployment", "web"),
		swData(source.DataOperationUpsert, "team-a", "web", "StatefulSet", "web-db"),
		swData(source.DataOperationUpsert, "team-b", "api", "Deployment", "api"),
	}
	assert.Equal(t, want, items)
}

func TestSyncServiceWorkloadRelationshipsErrors(t *testing.T) {
	setupFixedTime(t)

	t.Run("one kind failing does not stop the others", func(t *testing.T) {
		web := lbl("app", "web")
		s := newFakeSource(t, selecting("team-a", "web", web), labelledDeployment("team-a", "web", web), labelledStatefulSet("team-a", "db", web))
		fakeClient, ok := s.clientset.(*fake.Clientset)
		require.True(t, ok)
		fakeClient.PrependReactor("list", "statefulsets", func(k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("forbidden")
		})

		items, err := syncOne(t, (*Source).syncServiceWorkloadRelationships, s)
		require.ErrorIs(t, err, ErrRetrievingAssets)
		assert.Equal(t, []source.Data{swData(source.DataOperationUpsert, "team-a", "web", "Deployment", "web")}, items)
	})

	t.Run("services failing emits nothing", func(t *testing.T) {
		web := lbl("app", "web")
		s := newFakeSource(t, selecting("team-a", "web", web), labelledDeployment("team-a", "web", web))
		fakeClient, ok := s.clientset.(*fake.Clientset)
		require.True(t, ok)
		fakeClient.PrependReactor("list", "services", func(k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("forbidden")
		})

		items, err := syncOne(t, (*Source).syncServiceWorkloadRelationships, s)
		require.ErrorIs(t, err, ErrRetrievingAssets)
		assert.Empty(t, items)
	})

	t.Run("canceled while sending", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		web := lbl("app", "web")
		s := newFakeSource(t, selecting("team-a", "web", web), labelledDeployment("team-a", "web", web))

		done := make(chan error, 1)
		go func() { done <- s.syncServiceWorkloadRelationships(ctx, make(chan source.Data)) }()
		cancel()
		require.ErrorIs(t, <-done, context.Canceled)
	})
}

func TestStartSyncProcessServiceWorkloadRelationship(t *testing.T) {
	setupFixedTime(t)

	web := lbl("app", "web")
	s := newFakeSource(t, selecting("team-a", "web", web), labelledDeployment("team-a", "web", web))
	results := make(chan source.Data, 10)
	require.NoError(t, s.StartSyncProcess(t.Context(), map[string]source.Extra{swType: {}}, results))
	close(results)
	assert.Equal(t, []source.Data{swData(source.DataOperationUpsert, "team-a", "web", "Deployment", "web")}, collectData(results))
}

func TestServiceWorkloadRelationshipIsKnownAndStreamable(t *testing.T) {
	t.Parallel()

	assert.True(t, isStreamable(swType))
	assert.True(t, isKnownType(swType))
	assert.Equal(t, swType, knownTypes[len(knownTypes)-2].name)
	assert.Equal(t, workloadHelmReleaseRelationshipType, knownTypes[len(knownTypes)-3].name)
}

func TestStartEventStreamServiceWorkloadRelationships(t *testing.T) {
	web := lbl("app", "web")
	api := lbl("app", "api")
	const informers = 4 // services, deployments, statefulsets, daemonsets

	initial := func() []runtime.Object {
		return []runtime.Object{
			selecting("team-b", "api", api),
			selecting("team-a", "web", web),
			selecting("team-a", "external", nil),
			labelledDeployment("team-a", "web", web),
			labelledStatefulSet("team-a", "db", lbl("app", "web", "role", "db")),
			labelledDaemonSet("team-b", "agent", api),
			labelledDeployment("team-b", "web-elsewhere", web),
		}
	}

	t.Run("one initial emission once all informers synced, without the item types", func(t *testing.T) {
		harness := startStream(t, []string{swType}, initial()...)

		assert.Equal(t, []source.Data{
			swData(source.DataOperationUpsert, "team-a", "web", "Deployment", "web"),
			swData(source.DataOperationUpsert, "team-a", "web", "StatefulSet", "db"),
			swData(source.DataOperationUpsert, "team-b", "api", "DaemonSet", "agent"),
		}, harness.nextN(3))
		harness.waitWatches(informers)
		assert.Empty(t, harness.results, "no item and no duplicated pair")
	})

	t.Run("service selector edit", func(t *testing.T) {
		harness := startStream(t, []string{swType}, initial()...)
		harness.nextN(3)
		harness.waitWatches(informers)

		// unchanged selector: nothing is emitted.
		unchanged := selecting("team-a", "web", web)
		unchanged.Labels = lbl("x", "y")
		harness.update(servicesGVR, versioned(unchanged, "2"), "team-a")

		harness.update(servicesGVR, versioned(selecting("team-a", "web", lbl("role", "db")), "3"), "team-a")
		assert.Equal(t, []source.Data{swData(source.DataOperationDelete, "team-a", "web", "Deployment", "web")}, harness.nextN(1))
		assert.Empty(t, harness.results)
	})

	t.Run("selector edit adds and removes in one update", func(t *testing.T) {
		harness := startStream(t, []string{swType},
			selecting("team-a", "web", web), labelledDeployment("team-a", "old", web), labelledDeployment("team-a", "new", lbl("app", "next")))
		harness.nextN(1)
		harness.waitWatches(informers)

		harness.update(servicesGVR, versioned(selecting("team-a", "web", lbl("app", "next")), "2"), "team-a")
		assert.Equal(t, []source.Data{
			swData(source.DataOperationUpsert, "team-a", "web", "Deployment", "new"),
			swData(source.DataOperationDelete, "team-a", "web", "Deployment", "old"),
		}, harness.nextN(2))
		assert.Empty(t, harness.results)
	})

	t.Run("workload relabel", func(t *testing.T) {
		harness := startStream(t, []string{swType}, selecting("team-a", "web", web), selecting("team-a", "api", api), labelledDeployment("team-a", "svc", web))
		harness.nextN(1)
		harness.waitWatches(informers)

		harness.update(deploymentsGVR, versioned(labelledDeployment("team-a", "svc", lbl("app", "web", "app2", "x")), "2"), "team-a")
		harness.update(deploymentsGVR, versioned(labelledDeployment("team-a", "svc", api), "3"), "team-a")
		assert.Equal(t, []source.Data{
			swData(source.DataOperationUpsert, "team-a", "api", "Deployment", "svc"),
			swData(source.DataOperationDelete, "team-a", "web", "Deployment", "svc"),
		}, harness.nextN(2))

		harness.update(deploymentsGVR, versioned(labelledDeployment("team-a", "svc", nil), "4"), "team-a")
		assert.Equal(t, []source.Data{swData(source.DataOperationDelete, "team-a", "api", "Deployment", "svc")}, harness.nextN(1))
		assert.Empty(t, harness.results)
	})

	t.Run("workloads added and deleted", func(t *testing.T) {
		harness := startStream(t, []string{swType}, selecting("team-a", "web", web), selecting("team-a", "web2", web))
		harness.waitWatches(informers)

		harness.create(statefulSetsGVR, labelledStatefulSet("team-a", "db", web), "team-a")
		assert.Equal(t, []source.Data{
			swData(source.DataOperationUpsert, "team-a", "web", "StatefulSet", "db"),
			swData(source.DataOperationUpsert, "team-a", "web2", "StatefulSet", "db"),
		}, harness.nextN(2))

		harness.create(daemonSetsGVR, labelledDaemonSet("team-a", "agent", web), "team-a")
		harness.create(deploymentsGVR, labelledDeployment("team-b", "other-ns", web), "team-b")
		assert.Equal(t, []source.Data{
			swData(source.DataOperationUpsert, "team-a", "web", "DaemonSet", "agent"),
			swData(source.DataOperationUpsert, "team-a", "web2", "DaemonSet", "agent"),
		}, harness.nextN(2))

		harness.remove(statefulSetsGVR, "team-a", "db")
		assert.Equal(t, []source.Data{
			swData(source.DataOperationDelete, "team-a", "web", "StatefulSet", "db"),
			swData(source.DataOperationDelete, "team-a", "web2", "StatefulSet", "db"),
		}, harness.nextN(2))
		assert.Empty(t, harness.results)
	})

	t.Run("services added and deleted", func(t *testing.T) {
		harness := startStream(t, []string{swType}, labelledDeployment("team-a", "web", web), labelledStatefulSet("team-a", "db", web))
		harness.waitWatches(informers)

		harness.create(servicesGVR, selecting("team-a", "web", web), "team-a")
		assert.Equal(t, []source.Data{
			swData(source.DataOperationUpsert, "team-a", "web", "Deployment", "web"),
			swData(source.DataOperationUpsert, "team-a", "web", "StatefulSet", "db"),
		}, harness.nextN(2))

		// a service without selector never links.
		harness.create(servicesGVR, selecting("team-a", "external", nil), "team-a")
		harness.remove(servicesGVR, "team-a", "web")
		assert.Equal(t, []source.Data{
			swData(source.DataOperationDelete, "team-a", "web", "Deployment", "web"),
			swData(source.DataOperationDelete, "team-a", "web", "StatefulSet", "db"),
		}, harness.nextN(2))
		assert.Empty(t, harness.results)
	})

	t.Run("unrelated updates emit nothing", func(t *testing.T) {
		harness := startStream(t, []string{swType}, selecting("team-a", "web", web), labelledDeployment("team-a", "web", web))
		harness.nextN(1)
		harness.waitWatches(informers)

		dep := labelledDeployment("team-a", "web", web)
		dep.Annotations = map[string]string{"note": "x"}
		dep.Labels = lbl("meta", "only")
		replicas := int32(3)
		dep.Spec.Replicas = &replicas
		harness.update(deploymentsGVR, versioned(dep, "2"), "team-a")
		harness.update(servicesGVR, versioned(selecting("team-a", "web", web), "2"), "team-a")
		harness.create(deploymentsGVR, labelledDeployment("team-a", "nomatch", api), "team-a")
		harness.create(servicesGVR, selecting("team-a", "nomatch", lbl("app", "none")), "team-a")

		// the next item is the real change: nothing was emitted for the updates above.
		harness.update(deploymentsGVR, versioned(labelledDeployment("team-a", "web", nil), "3"), "team-a")
		assert.Equal(t, []source.Data{swData(source.DataOperationDelete, "team-a", "web", "Deployment", "web")}, harness.nextN(1))
		assert.Empty(t, harness.results)
	})

	t.Run("namespaces are isolated", func(t *testing.T) {
		harness := startStream(t, []string{swType}, selecting("team-a", "web", web), labelledDeployment("team-b", "web", web))
		harness.waitWatches(informers)

		harness.create(deploymentsGVR, labelledDeployment("team-c", "web", web), "team-c")
		harness.create(servicesGVR, selecting("team-d", "web", web), "team-d")
		harness.create(statefulSetsGVR, labelledStatefulSet("team-c", "db", web), "team-c")
		harness.create(deploymentsGVR, labelledDeployment("team-a", "web", web), "team-a")
		assert.Equal(t, []source.Data{swData(source.DataOperationUpsert, "team-a", "web", "Deployment", "web")}, harness.nextN(1))
		assert.Empty(t, harness.results)
	})

	t.Run("item and relationship types share the informers", func(t *testing.T) {
		harness := startStream(t, []string{serviceType, deploymentType, swType},
			selecting("team-a", "web", web), labelledDeployment("team-a", "web", web))

		types := map[string]int{}
		for _, data := range harness.nextN(3) {
			types[data.Type]++
		}
		assert.Equal(t, map[string]int{serviceType: 1, deploymentType: 1, swType: 1}, types)
		harness.waitWatches(informers)

		lists, watches := map[string]int{}, map[string]int{}
		for _, action := range harness.client.Actions() {
			switch action.GetVerb() {
			case "list":
				lists[action.GetResource().Resource]++
			case "watch":
				watches[action.GetResource().Resource]++
			}
		}
		want := map[string]int{"services": 1, "deployments": 1, "statefulsets": 1, "daemonsets": 1}
		assert.Equal(t, want, lists)
		assert.Equal(t, want, watches)
	})

	t.Run("stops on cancellation", func(t *testing.T) {
		harness := startStream(t, []string{swType}, initial()...)
		harness.nextN(3)
		harness.waitWatches(informers)
		harness.shutdown()
	})

	t.Run("stops while sending the initial pairs", func(t *testing.T) {
		harness := startStream(t, []string{swType}, initial()...)
		harness.waitWatches(informers)
		harness.shutdown()
	})
}

// newTestReconciler returns the service/workload reconciler over empty caches
// and a results channel of the given capacity.
func newTestReconciler(t *testing.T, ctx context.Context, capacity int) (*pairReconciler, map[string]cache.Indexer, chan source.Data) {
	t.Helper()
	setupFixedTime(t)

	results := make(chan source.Data, capacity)
	stream := &eventStream{ctx: ctx, log: logger.FromContext(ctx), apiServer: testAPIServer, results: results}
	reconciler := newServiceWorkloadReconciler(stream)
	for _, src := range serviceWorkloadSources {
		reconciler.indexers[src.kind] = cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	}
	return reconciler, reconciler.indexers, results
}

func addAll(t *testing.T, indexer cache.Indexer, objects ...any) {
	t.Helper()
	for _, obj := range objects {
		require.NoError(t, indexer.Add(obj))
	}
}

func TestPairReconcilerWaitsForEveryInformer(t *testing.T) {
	web := lbl("app", "web")
	reconciler, indexers, results := newTestReconciler(t, t.Context(), 10)
	addAll(t, indexers[serviceType], selecting("team-a", "web", web))
	addAll(t, indexers[deploymentType], labelledDeployment("team-a", "web", web))

	services := reconciler.handlerFor(serviceType)
	deployments := reconciler.handlerFor(deploymentType)

	// events before the initial emission are ignored, even when the other informers synced.
	deployments.OnAdd(labelledDeployment("team-a", "web", web), false)
	services.OnDelete(selecting("team-a", "web", web))

	reconciler.informerSynced(serviceType)
	reconciler.informerSynced(deploymentType)
	reconciler.informerSynced(deploymentType)
	reconciler.informerSynced(statefulSetType)
	assert.Empty(t, results)

	reconciler.informerSynced(daemonSetType)
	require.Len(t, results, 1)
	assert.Equal(t, swData(source.DataOperationUpsert, "team-a", "web", "Deployment", "web"), <-results)

	// once ready, a repeated sync notification emits nothing and the initial list adds are ignored.
	reconciler.informerSynced(daemonSetType)
	deployments.OnAdd(labelledDeployment("team-a", "web", web), true)
	assert.Empty(t, results)

	// a live add is reconciled against the caches.
	addAll(t, indexers[statefulSetType], labelledStatefulSet("team-a", "db", web))
	reconciler.handlerFor(statefulSetType).OnAdd(labelledStatefulSet("team-a", "db", web), false)
	require.Len(t, results, 1)
	assert.Equal(t, swData(source.DataOperationUpsert, "team-a", "web", "StatefulSet", "db"), <-results)

	// the same state again: identical sets emit nothing.
	reconciler.handlerFor(statefulSetType).OnAdd(labelledStatefulSet("team-a", "db", web), false)
	assert.Empty(t, results)
}

func TestPairReconcilerHandlers(t *testing.T) {
	web := lbl("app", "web")
	reconciler, indexers, results := newTestReconciler(t, t.Context(), 10)
	svc := selecting("team-a", "web", web)
	dep := labelledDeployment("team-a", "web", web)
	addAll(t, indexers[serviceType], svc)
	addAll(t, indexers[deploymentType], dep)
	for _, src := range serviceWorkloadSources {
		reconciler.informerSynced(src.kind)
	}
	require.Len(t, results, 1)
	<-results

	assert.Nil(t, reconciler.handlerFor("unknown"))
	assert.Empty(t, (&eventStream{}).newReconcilers(map[string]struct{}{namespaceType: {}}))
	handler := reconciler.handlerFor(deploymentType)

	// unexpected objects, same resource version and unchanged labels are skipped.
	handler.OnAdd("unexpected", false)
	handler.OnUpdate(versioned(dep.DeepCopy(), "1"), versioned(labelledDeployment("team-a", "web", nil), "1"))
	handler.OnUpdate(versioned(dep.DeepCopy(), "1"), versioned(dep.DeepCopy(), "2"))
	handler.OnDelete(cache.DeletedFinalStateUnknown{Key: "x", Obj: "unexpected"})
	assert.Empty(t, results)

	// a tombstone deletes the pairs of the last known object.
	require.NoError(t, indexers[deploymentType].Delete(dep))
	handler.OnDelete(cache.DeletedFinalStateUnknown{Key: "team-a/web", Obj: dep})
	require.Len(t, results, 1)
	assert.Equal(t, swData(source.DataOperationDelete, "team-a", "web", "Deployment", "web"), <-results)

	// the pair is gone from the state: deleting again emits nothing.
	handler.OnDelete(dep)
	assert.Empty(t, results)
}

func TestPairReconcilerConcurrentHandlers(t *testing.T) {
	web := lbl("app", "web")
	reconciler, indexers, results := newTestReconciler(t, t.Context(), 1000)
	addAll(t, indexers[serviceType], selecting("team-a", "web", web))
	for _, src := range serviceWorkloadSources {
		reconciler.informerSynced(src.kind)
	}

	var wg sync.WaitGroup
	for _, dataType := range []string{deploymentType, statefulSetType, daemonSetType} {
		wg.Go(func() {
			handler := reconciler.handlerFor(dataType)
			for range 20 {
				handler.OnAdd(labelledDeployment("team-a", "x", web), false)
				handler.OnUpdate(versioned(labelledDeployment("team-a", "x", web), "1"), versioned(labelledDeployment("team-a", "x", nil), "2"))
			}
		})
	}
	wg.Wait()
	assert.Empty(t, results)
}

func TestPairReconcilerStopsWhileSending(t *testing.T) {
	web := lbl("app", "web")
	ctx, cancel := context.WithCancel(t.Context())
	reconciler, indexers, results := newTestReconciler(t, ctx, 0)
	addAll(t, indexers[serviceType], selecting("team-a", "web", web))
	addAll(t, indexers[deploymentType], labelledDeployment("team-a", "web", web))
	cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, src := range serviceWorkloadSources {
			reconciler.informerSynced(src.kind)
		}
	}()
	<-done
	assert.Empty(t, results)
}

func TestBindReconcilers(t *testing.T) {
	setupFixedTime(t)
	stream := &eventStream{ctx: t.Context(), log: nilLogger(t), apiServer: testAPIServer, results: make(chan source.Data, 1)}

	client := fake.NewClientset()
	factory := informers.NewSharedInformerFactory(client, informerResync)
	informer := factory.Core().V1().Namespaces().Informer()
	assert.Nil(t, stream.bindReconcilers(namespaceType, informer), "no reconciler")

	stream.reconcilers = []*pairReconciler{newServiceWorkloadReconciler(stream)}
	assert.Nil(t, stream.bindReconcilers(namespaceType, informer), "kind not depended on")
	assert.NotNil(t, stream.bindReconcilers(serviceType, factory.Core().V1().Services().Informer()))
	assert.Contains(t, stream.reconcilers[0].indexers, serviceType)

	handlers := stream.sourceHandlers(findEventKind(t, deploymentType), map[string]struct{}{swType: {}})
	assert.Len(t, handlers, 1, "the relationship type alone still needs the workload informers")
	assert.Empty(t, stream.sourceHandlers(findEventKind(t, namespaceType), map[string]struct{}{swType: {}}))
}

func podOwnerPair(namespace, pod, ownerKind, ownerName string) map[string]any {
	return map[string]any{
		"apiServer": testAPIServer,
		"namespace": namespace,
		"name":      pod,
		"ownerKind": ownerKind,
		"ownerName": ownerName,
	}
}

func TestPodOwnerPairs(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		pod  *corev1.Pod
		want []map[string]any
	}{
		"deployment through replicaset": {
			pod:  newPod("n", "web-abc-1", withLabels("pod-template-hash", "abc"), ownedBy("ReplicaSet", "web-abc", boolPtr(true))),
			want: []map[string]any{podOwnerPair("n", "web-abc-1", "Deployment", "web")},
		},
		"statefulset": {
			pod:  newPod("n", "db-0", ownedBy("StatefulSet", "db", boolPtr(true))),
			want: []map[string]any{podOwnerPair("n", "db-0", "StatefulSet", "db")},
		},
		"daemonset": {
			pod:  newPod("n", "agent-x", ownedBy("DaemonSet", "agent", nil)),
			want: []map[string]any{podOwnerPair("n", "agent-x", "DaemonSet", "agent")},
		},
		"replicaset without hash": {pod: newPod("n", "p", ownedBy("ReplicaSet", "web-abc", nil))},
		"job":                     {pod: newPod("n", "p", ownedBy("Job", "batch", nil))},
		"no owner":                {pod: newPod("n", "p")},
		"controller is a job":     {pod: newPod("n", "p", ownedBy("DaemonSet", "agent", nil), ownedBy("Job", "batch", boolPtr(true)))},
		"custom controller kind":  {pod: newPod("n", "p", ownedBy("Rollout", "web", boolPtr(true)))},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := podOwnerPairs(test.pod, testAPIServer)
			if test.want == nil {
				assert.Empty(t, got)
				return
			}
			assert.Equal(t, test.want, pairValues(got))
		})
	}
}

func TestSyncPodOwnerRelationships(t *testing.T) {
	setupFixedTime(t)

	s := newFakeSource(t,
		newPod("team-a", "web-abc-1", withLabels("pod-template-hash", "abc"), ownedBy("ReplicaSet", "web-abc", boolPtr(true))),
		newPod("team-a", "db-0", ownedBy("StatefulSet", "db", boolPtr(true))),
		newPod("team-a", "job-1", ownedBy("Job", "batch", boolPtr(true))),
		newPod("team-a", "bare"),
		newPod("team-a", "done-1", withPhase(corev1.PodSucceeded), ownedBy("DaemonSet", "agent", boolPtr(true))),
		newPod("team-a", "agent-1", withPhase(corev1.PodFailed), ownedBy("DaemonSet", "agent", boolPtr(true))),
	)

	items, err := syncOne(t, (*Source).syncPodOwnerRelationships, s)
	require.NoError(t, err)

	got := map[string]source.Data{}
	for _, item := range items {
		assert.Equal(t, podOwnerRelationshipType, item.Type)
		assert.Equal(t, source.DataOperationUpsert, item.Operation)
		assert.Equal(t, testFixedTime, item.Time)
		got[item.Values["name"].(string)] = item //nolint:forcetypeassert // always a string
	}
	require.Len(t, got, 3)
	assert.Equal(t, podOwnerPair("team-a", "web-abc-1", "Deployment", "web"), got["web-abc-1"].Values)
	assert.Equal(t, podOwnerPair("team-a", "db-0", "StatefulSet", "db"), got["db-0"].Values)
	assert.Equal(t, podOwnerPair("team-a", "agent-1", "DaemonSet", "agent"), got["agent-1"].Values)
}

func TestSyncPodOwnerRelationshipsErrors(t *testing.T) {
	setupFixedTime(t)

	t.Run("list error", func(t *testing.T) {
		s := newFakeSource(t, newPod("team-a", "db-0", ownedBy("StatefulSet", "db", boolPtr(true))))
		fakeClient, ok := s.clientset.(*fake.Clientset)
		require.True(t, ok)
		fakeClient.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("forbidden")
		})

		items, err := syncOne(t, (*Source).syncPodOwnerRelationships, s)
		require.ErrorIs(t, err, ErrRetrievingAssets)
		assert.Empty(t, items)
	})

	t.Run("canceled while sending", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		s := newFakeSource(t, newPod("team-a", "db-0", ownedBy("StatefulSet", "db", boolPtr(true))))

		done := make(chan error, 1)
		go func() { done <- s.syncPodOwnerRelationships(ctx, make(chan source.Data)) }()
		cancel()
		require.ErrorIs(t, <-done, context.Canceled)
	})
}
