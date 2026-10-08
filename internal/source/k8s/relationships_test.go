// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package k8s

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"

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

	last := knownTypes[len(knownTypes)-3:]
	assert.Equal(t, ingressRouteServiceRelationshipType, last[0].name)
	assert.Equal(t, workloadHelmReleaseRelationshipType, last[1].name)
	assert.Equal(t, serviceWorkloadRelationshipType, last[2].name)
	assert.Equal(t, networkPolicyType, knownTypes[len(knownTypes)-4].name)
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
