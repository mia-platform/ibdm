// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package k8s

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"

	"github.com/mia-platform/ibdm/internal/source"
)

var nodesGVR = corev1.SchemeGroupVersion.WithResource("nodes")

// serving configures a fake discovery to serve lists.
func serving(lists ...*metav1.APIResourceList) func(*fakediscovery.FakeDiscovery) {
	return func(discovery *fakediscovery.FakeDiscovery) { discovery.Resources = lists }
}

// versioned returns obj with the given resource version.
func versioned[T interface {
	runtime.Object
	metav1.Object
}](obj T, version string) T {
	obj.SetResourceVersion(version)
	return obj
}

func TestEventHandlersNoOpUpdates(t *testing.T) {
	pod := func(image string) corev1.PodSpec {
		return testPodSpec([]corev1.Container{testContainer("app", image)}, nil)
	}
	labels := map[string]string{"app": "web"}
	withResources := func(requests, limits corev1.ResourceList) corev1.PodSpec {
		container := testContainer("app", "img:1")
		container.Resources = corev1.ResourceRequirements{Requests: requests, Limits: limits}
		return testPodSpec([]corev1.Container{container}, nil)
	}
	cpu := func(quantity string) corev1.ResourceList {
		return corev1.ResourceList{corev1.ResourceCPU: resource.MustParse(quantity)}
	}

	type variant = func() runtime.Object
	tests := map[string]struct {
		kind    string
		old     variant
		noop    []variant
		changed []variant
	}{
		"deployment": {
			kind: deploymentType,
			old: func() runtime.Object {
				return versioned(newDeployment("team-a", "web", labels, int32Ptr(1), pod("img:1")), "1")
			},
			noop: []variant{
				func() runtime.Object {
					obj := newDeployment("team-a", "web", labels, int32Ptr(1), pod("img:1"))
					obj.Status.ReadyReplicas = 1
					obj.Status.ObservedGeneration = 4
					return versioned(obj, "2")
				},
			},
			changed: []variant{
				func() runtime.Object {
					return versioned(newDeployment("team-a", "web", labels, int32Ptr(3), pod("img:1")), "2")
				},
				func() runtime.Object {
					return versioned(newDeployment("team-a", "web", labels, int32Ptr(1), pod("img:2")), "2")
				},
				func() runtime.Object {
					return versioned(newDeployment("team-a", "web", map[string]string{"app": "api"}, int32Ptr(1), pod("img:1")), "2")
				},
				func() runtime.Object {
					return versioned(newDeployment("team-a", "web", labels, int32Ptr(1), withResources(cpu("1"), nil)), "2")
				},
				func() runtime.Object {
					return versioned(newDeployment("team-a", "web", labels, int32Ptr(1), withResources(nil, cpu("1"))), "2")
				},
			},
		},
		"statefulset": {
			kind: statefulSetType,
			old: func() runtime.Object {
				return versioned(newStatefulSet("team-a", "db", labels, int32Ptr(1), pod("img:1")), "1")
			},
			noop: []variant{
				func() runtime.Object {
					obj := newStatefulSet("team-a", "db", labels, int32Ptr(1), pod("img:1"))
					obj.Status.ReadyReplicas = 1
					return versioned(obj, "2")
				},
			},
			changed: []variant{
				func() runtime.Object {
					return versioned(newStatefulSet("team-a", "db", labels, int32Ptr(2), pod("img:1")), "2")
				},
				func() runtime.Object {
					return versioned(newStatefulSet("team-a", "db", labels, int32Ptr(1), pod("img:2")), "2")
				},
				func() runtime.Object {
					return versioned(newStatefulSet("team-a", "db", map[string]string{"x": "y"}, int32Ptr(1), pod("img:1")), "2")
				},
				func() runtime.Object {
					return versioned(newStatefulSet("team-a", "db", labels, int32Ptr(1), withResources(cpu("1"), nil)), "2")
				},
			},
		},
		"daemonset": {
			kind: daemonSetType,
			old:  func() runtime.Object { return versioned(newDaemonSet("team-a", "agent", labels, pod("img:1")), "1") },
			noop: []variant{
				func() runtime.Object {
					obj := newDaemonSet("team-a", "agent", labels, pod("img:1"))
					obj.Status.NumberReady = 5
					return versioned(obj, "2")
				},
			},
			changed: []variant{
				func() runtime.Object { return versioned(newDaemonSet("team-a", "agent", labels, pod("img:2")), "2") },
				func() runtime.Object {
					return versioned(newDaemonSet("team-a", "agent", map[string]string{"x": "y"}, pod("img:1")), "2")
				},
				func() runtime.Object {
					return versioned(newDaemonSet("team-a", "agent", labels, withResources(nil, cpu("1"))), "2")
				},
			},
		},
		"service": {
			kind: serviceType,
			old: func() runtime.Object {
				return versioned(newService("team-a", "web-svc", labels, corev1.ServiceSpec{}), "1")
			},
			noop: []variant{
				func() runtime.Object {
					obj := newService("team-a", "web-svc", labels, corev1.ServiceSpec{})
					obj.Annotations = map[string]string{"note": "not sent"}
					return versioned(obj, "2")
				},
			},
			changed: []variant{
				func() runtime.Object {
					return versioned(newService("team-a", "web-svc", labels, corev1.ServiceSpec{ClusterIP: "10.0.0.1"}), "2")
				},
				func() runtime.Object {
					return versioned(newService("team-a", "web-svc", map[string]string{"x": "y"}, corev1.ServiceSpec{}), "2")
				},
			},
		},
		"namespace": {
			kind: namespaceType,
			old: func() runtime.Object {
				return versioned(newNamespace("team-a", labels), "1")
			},
			noop: []variant{
				func() runtime.Object { return versioned(newNamespace("team-a", labels), "2") },
			},
			changed: []variant{
				func() runtime.Object { return versioned(newNamespace("team-a", map[string]string{"x": "y"}), "2") },
				func() runtime.Object {
					obj := newNamespace("team-a", labels)
					obj.Status.Phase = corev1.NamespaceTerminating
					return versioned(obj, "2")
				},
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			setupFixedTime(t)
			results := make(chan source.Data, 10)
			stream := &eventStream{ctx: t.Context(), log: nilLogger(t), apiServer: testAPIServer, results: results}
			handler := stream.handlers(findEventKind(t, test.kind))

			for _, build := range test.noop {
				handler.OnUpdate(test.old(), build())
			}
			assert.Empty(t, results)

			for _, build := range test.changed {
				handler.OnUpdate(test.old(), build())
				require.Len(t, results, 1)
				data := <-results
				assert.Equal(t, source.DataOperationUpsert, data.Operation)
				assert.Equal(t, test.kind, data.Type)
			}
		})
	}
}

func TestEventHandlersHelmNoOpUpdates(t *testing.T) {
	setupFixedTime(t)
	results := make(chan source.Data, 10)
	stream := &eventStream{ctx: t.Context(), log: nilLogger(t), apiServer: testAPIServer, results: results}
	informer := informers.NewSharedInformerFactory(fake.NewClientset(), 0).Core().V1().Secrets().Informer()
	handler := stream.helmHandlers(informer)

	old := versioned(helmSecret(t, "team-a", "shop", 1, "deployed"), "1")

	// same release content, only metadata churn.
	same := helmSecret(t, "team-a", "shop", 1, "deployed")
	same.Annotations = map[string]string{"note": "churn"}
	handler.OnUpdate(old, versioned(same, "2"))
	assert.Empty(t, results)

	// a different release content is emitted.
	handler.OnUpdate(old, versioned(helmSecret(t, "team-a", "shop", 2, "deployed"), "3"))
	require.Len(t, results, 1)
	assert.Equal(t, 2, (<-results).Values["revision"])

	// an undecodable side never counts as unchanged.
	broken := newHelmSecret("team-a", "shop", 1, "deployed", []byte("not base64 !!"))
	handler.OnUpdate(versioned(broken, "1"), versioned(helmSecret(t, "team-a", "shop", 1, "deployed"), "2"))
	require.Len(t, results, 1)
	<-results
	handler.OnUpdate(old, versioned(newHelmSecret("team-a", "shop", 1, "deployed", []byte("not base64 !!")), "4"))
	assert.Empty(t, results)

	// a Secret entering the deployed set is emitted even with the same content.
	handler.OnUpdate(versioned(helmSecret(t, "team-a", "shop", 1, "pending-upgrade"), "1"), versioned(helmSecret(t, "team-a", "shop", 1, "deployed"), "2"))
	assert.Len(t, results, 1)
}

func TestWithoutNamespaceResourceVersion(t *testing.T) {
	t.Parallel()

	values, err := namespaceValues(versioned(newNamespace("team-a", nil), "9"), testAPIServer)
	require.NoError(t, err)

	trimmed := withoutNamespaceResourceVersion(values)
	assert.NotContains(t, trimmed[keyNamespace].(map[string]any)["metadata"], "resourceVersion")
	assert.Equal(t, "9", values[keyNamespace].(map[string]any)["metadata"].(map[string]any)["resourceVersion"], "input must not be modified")

	unexpected := map[string]any{keyNamespace: "x"}
	assert.Equal(t, unexpected, withoutNamespaceResourceVersion(unexpected))
	noMetadata := map[string]any{keyNamespace: map[string]any{}}
	assert.Equal(t, noMetadata, withoutNamespaceResourceVersion(noMetadata))
}

// crdCase describes how to build the objects of a CRD-backed type in tests.
type crdCase struct {
	kind  crdKind
	gvr   schema.GroupVersionResource
	build func(variant, annotation string) *unstructured.Unstructured
}

func crdCases() map[string]crdCase {
	return map[string]crdCase{
		"ingressroute": {
			kind: ingressRouteKind,
			gvr:  traefikV1Alpha1,
			build: func(variant, annotation string) *unstructured.Unstructured {
				obj := newIngressRouteObject("team-a", "web", map[string]any{"v": variant}, map[string]any{"entryPoints": []any{"web"}})
				setAnnotation(obj, annotation)
				return obj
			},
		},
		"certificate": {
			kind: certificateKind,
			gvr:  certManagerV1,
			build: func(variant, annotation string) *unstructured.Unstructured {
				obj := newCertificateObject("team-a", map[string]any{"dnsNames": []any{variant + ".example.com"}}, nil)
				setAnnotation(obj, annotation)
				return obj
			},
		},
	}
}

func setAnnotation(obj *unstructured.Unstructured, annotation string) {
	if annotation != "" {
		obj.SetAnnotations(map[string]string{"note": annotation})
	}
}

func TestStartEventStreamCRDSnapshot(t *testing.T) {
	route := newIngressRouteObject("team-a", "web", nil, nil)
	cert := newCertificateObject("team-a", nil, nil)
	harness := startStreamWithCRDs(t, []string{ingressRouteType, certificateType}, serving(allServed()...), []runtime.Object{route, cert})

	byType := make(map[string]source.Data)
	for _, data := range harness.nextN(2) {
		byType[data.Type] = data
	}
	require.Len(t, byType, 2)

	assert.Equal(t, source.Data{Type: ingressRouteType, Operation: source.DataOperationUpsert, Values: ingressRouteKind.objectValues(route, testAPIServer), Time: testFixedTime}, byType[ingressRouteType])
	assert.Equal(t, source.Data{Type: certificateType, Operation: source.DataOperationUpsert, Values: certificateKind.objectValues(cert, testAPIServer), Time: testFixedTime}, byType[certificateType])
	harness.waitWatches(2)
}

func TestStartEventStreamCRDLiveChanges(t *testing.T) {
	for name, test := range crdCases() {
		t.Run(name, func(t *testing.T) {
			harness := startStreamWithCRDs(t, []string{test.kind.dataType}, serving(allServed()...), nil)
			harness.waitWatches(1)
			resourceClient := harness.dynamic.Resource(test.gvr).Namespace("team-a")

			created := test.build("one", "")
			_, err := resourceClient.Create(t.Context(), created, metav1.CreateOptions{})
			require.NoError(t, err)
			wantCreated := test.kind.objectValues(created, testAPIServer)
			assert.Equal(t, source.Data{Type: test.kind.dataType, Operation: source.DataOperationUpsert, Values: wantCreated, Time: testFixedTime}, harness.next())

			// an update of a field that is not emitted produces nothing: the next item is the real change.
			_, err = resourceClient.Update(t.Context(), test.build("one", "annotated"), metav1.UpdateOptions{})
			require.NoError(t, err)
			updated := test.build("two", "annotated")
			_, err = resourceClient.Update(t.Context(), updated, metav1.UpdateOptions{})
			require.NoError(t, err)
			wantUpdated := test.kind.objectValues(updated, testAPIServer)
			data := harness.next()
			assert.Equal(t, source.DataOperationUpsert, data.Operation)
			assert.Equal(t, wantUpdated, data.Values)
			assert.NotEqual(t, wantCreated, data.Values)

			require.NoError(t, resourceClient.Delete(t.Context(), created.GetName(), metav1.DeleteOptions{}))
			data = harness.next()
			assert.Equal(t, source.DataOperationDelete, data.Operation)
			assert.Equal(t, test.kind.dataType, data.Type)
			assert.Equal(t, wantUpdated, data.Values)
			assert.Empty(t, harness.results)
		})
	}
}

func TestEventHandlersCRD(t *testing.T) {
	setupFixedTime(t)

	for name, test := range crdCases() {
		t.Run(name, func(t *testing.T) {
			results := make(chan source.Data, 10)
			stream := &eventStream{ctx: t.Context(), log: nilLogger(t), apiServer: testAPIServer, results: results}
			handler := stream.handlers(test.kind.eventKind())

			obj := test.build("one", "")
			handler.OnDelete(cache.DeletedFinalStateUnknown{Key: "team-a/x", Obj: obj})
			require.Len(t, results, 1)
			data := <-results
			assert.Equal(t, source.DataOperationDelete, data.Operation)
			assert.Equal(t, test.kind.objectValues(obj, testAPIServer), data.Values)

			// unexpected objects are ignored without panicking.
			var nilObject *unstructured.Unstructured
			handler.OnAdd(&corev1.Pod{}, false)
			handler.OnAdd(nilObject, false)
			handler.OnUpdate("garbage", &corev1.Pod{})
			handler.OnDelete(cache.DeletedFinalStateUnknown{Key: "team-a/x", Obj: "garbage"})
			handler.OnDelete(&corev1.Pod{})
			assert.Empty(t, results)
		})
	}
}

func TestStartEventStreamCRDNotInstalledIsSkipped(t *testing.T) {
	route := newIngressRouteObject("team-a", "web", nil, nil)
	cert := newCertificateObject("team-a", nil, nil)
	harness := startStreamWithCRDs(t,
		[]string{ingressRouteType, certificateType, namespaceType},
		serving(serverResources("cert-manager.io/v1", certificatesRes)),
		[]runtime.Object{route, cert},
		newNamespace("team-a", nil),
	)

	types := []string{harness.next().Type, harness.next().Type}
	assert.ElementsMatch(t, []string{namespaceType, certificateType}, types)
	harness.waitWatches(2)
	assert.Empty(t, harness.results)
}

func TestStartEventStreamCRDDiscoveryErrorIsolation(t *testing.T) {
	t.Run("one type fails", func(t *testing.T) {
		var calls atomic.Int32
		harness := startStreamWithCRDs(t, []string{ingressRouteType, certificateType},
			func(discovery *fakediscovery.FakeDiscovery) {
				discovery.Resources = allServed()
				// the ingressroute type is resolved first: only its group version lookup fails.
				discovery.PrependReactor("get", "resource", func(k8stesting.Action) (bool, runtime.Object, error) {
					if calls.Add(1) == 1 {
						return true, nil, errors.New("discovery down")
					}
					return false, nil, nil
				})
			},
			[]runtime.Object{newIngressRouteObject("team-a", "web", nil, nil), newCertificateObject("team-a", nil, nil)},
		)

		assert.Equal(t, certificateType, harness.next().Type)
		harness.waitWatches(1)
		assert.Empty(t, harness.results)
	})

	t.Run("every discovery call fails", func(t *testing.T) {
		harness := startStreamWithCRDs(t, []string{ingressRouteType, certificateType, namespaceType},
			func(discovery *fakediscovery.FakeDiscovery) {
				discovery.Resources = allServed()
				discovery.PrependReactor("get", "group", func(k8stesting.Action) (bool, runtime.Object, error) {
					return true, nil, errors.New("discovery down")
				})
			},
			nil,
			newNamespace("team-a", nil),
		)

		assert.Equal(t, namespaceType, harness.next().Type)
		harness.waitWatches(1)
		assert.Empty(t, harness.results)
	})
}

func TestStartEventStreamCRDOnlyNotInstalledReturns(t *testing.T) {
	setupFixedTime(t)
	s := newCRDSource(t, nil)
	results := make(chan source.Data, 1)

	require.NoError(t, s.StartEventStream(t.Context(), map[string]source.Extra{ingressRouteType: nil, certificateType: nil}, results))
	assert.Empty(t, results)
}

func TestStartEventStreamCRDDiscoveryCancelled(t *testing.T) {
	setupFixedTime(t)
	s := newCRDSource(t, allServed())
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	results := make(chan source.Data, 1)
	require.NoError(t, s.StartEventStream(ctx, map[string]source.Extra{ingressRouteType: nil}, results))
	assert.Empty(t, results)
}

// testNode builds a node with the given CPU capacity and taints.
func testNode(name, cpu string, taints ...corev1.Taint) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       corev1.NodeSpec{Taints: taints},
		Status: corev1.NodeStatus{
			Capacity: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse(cpu)},
			NodeInfo: corev1.NodeSystemInfo{KubeletVersion: "v1.30.0"},
		},
	}
}

func TestStartEventStreamClusterSingleInitialUpsert(t *testing.T) {
	harness := startStream(t, []string{clusterType},
		testNode("node-b", "2"), testNode("node-a", "4"), testNode("node-c", "500m"),
	)

	first := harness.next()
	assert.Equal(t, source.DataOperationUpsert, first.Operation)
	assert.Equal(t, clusterType, first.Type)
	assert.Equal(t, testFixedTime, first.Time)
	assert.Equal(t, testAPIServer, first.Values[keyAPIServer])
	assert.Equal(t, testClusterName, first.Values["clusterName"])
	assert.Equal(t, 3, first.Values["nodeCount"])
	assert.InDelta(t, 6.5, first.Values["totalCPUCores"], 0.0001)
	assert.Equal(t, clusterValues(testAPIServer, testClusterName, []corev1.Node{*testNode("node-b", "2"), *testNode("node-a", "4"), *testNode("node-c", "500m")}), first.Values)
	harness.waitWatches(1)

	// the next item is the change below, not one more upsert per initial node.
	harness.create(nodesGVR, testNode("node-d", "1"), "")
	second := harness.next()
	assert.Equal(t, 4, second.Values["nodeCount"])
	assert.InDelta(t, 7.5, second.Values["totalCPUCores"], 0.0001)
	assert.Empty(t, harness.results)
	assert.Empty(t, harness.watches, "only the node informer is started")
}

func TestStartEventStreamClusterChanges(t *testing.T) {
	harness := startStream(t, []string{clusterType}, testNode("node-a", "4"), testNode("node-b", "2"))
	require.Equal(t, 2, harness.next().Values["nodeCount"])
	harness.waitWatches(1)

	// condition and image churn does not change the cluster values.
	churn := testNode("node-a", "4")
	churn.Status.Conditions = []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue, Reason: "KubeletReady"}}
	churn.Status.Images = []corev1.ContainerImage{{Names: []string{"img:1"}}}
	harness.update(nodesGVR, churn, "")

	// taint change.
	tainted := testNode("node-a", "4", corev1.Taint{Key: "dedicated", Value: "gpu", Effect: corev1.TaintEffectNoSchedule})
	harness.update(nodesGVR, tainted, "")
	data := harness.next()
	assert.Equal(t, source.DataOperationUpsert, data.Operation)
	nodes := data.Values["nodes"].([]map[string]any)
	assert.Equal(t, []map[string]any{{"key": "dedicated", "value": "gpu", "effect": "NoSchedule"}}, nodes[0]["taints"])

	// capacity change.
	harness.update(nodesGVR, testNode("node-a", "8", tainted.Spec.Taints...), "")
	data = harness.next()
	assert.InDelta(t, 10.0, data.Values["totalCPUCores"], 0.0001)

	// node removal.
	harness.remove(nodesGVR, "", "node-b")
	data = harness.next()
	assert.Equal(t, 1, data.Values["nodeCount"])
	assert.InDelta(t, 8.0, data.Values["totalCPUCores"], 0.0001)
	assert.Empty(t, harness.results)
}

func TestStartEventStreamClusterIsNeverDeleted(t *testing.T) {
	harness := startStream(t, []string{clusterType}, testNode("node-a", "4"))
	require.Equal(t, 1, harness.next().Values["nodeCount"])
	harness.waitWatches(1)

	harness.remove(nodesGVR, "", "node-a")
	data := harness.next()
	assert.Equal(t, source.DataOperationUpsert, data.Operation)
	assert.Equal(t, 0, data.Values["nodeCount"])
	assert.Equal(t, []map[string]any{}, data.Values["nodes"])
	assert.InDelta(t, 0.0, data.Values["totalCPUCores"], 0.0001)

	harness.create(nodesGVR, testNode("node-b", "2"), "")
	data = harness.next()
	assert.Equal(t, source.DataOperationUpsert, data.Operation)
	assert.Equal(t, 1, data.Values["nodeCount"])
}

func TestStartEventStreamClusterStopsOnCancellation(t *testing.T) {
	harness := startStream(t, []string{clusterType, namespaceType}, testNode("node-a", "4"))
	harness.waitWatches(2)
	harness.next()
	harness.shutdown()
}

func TestClusterEmitter(t *testing.T) {
	setupFixedTime(t)
	results := make(chan source.Data, 10)
	stream := &eventStream{ctx: t.Context(), log: nilLogger(t), apiServer: testAPIServer, clusterName: testClusterName, results: results}
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, indexer.Add(testNode("node-a", "4")))
	require.NoError(t, indexer.Add(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "not-a-node"}}))

	emitter := &clusterEmitter{stream: stream, indexer: indexer}
	handler := emitter.handlers()

	// nothing is emitted before the initial sync, whatever the event.
	handler.OnAdd(testNode("node-b", "2"), false)
	handler.OnAdd(testNode("node-c", "2"), true)
	handler.OnUpdate(testNode("node-a", "4"), testNode("node-a", "8"))
	handler.OnDelete(testNode("node-a", "4"))
	assert.Empty(t, results)

	emitter.emitInitial()
	require.Len(t, results, 1)
	data := <-results
	assert.Equal(t, 1, data.Values["nodeCount"])

	// initial list adds and unchanged values are ignored; a same resource version is skipped.
	handler.OnAdd(testNode("node-c", "2"), true)
	handler.OnAdd(testNode("node-a", "4"), false)
	handler.OnUpdate(versioned(testNode("node-a", "4"), "1"), versioned(testNode("node-a", "9"), "1"))
	handler.OnDelete(cache.DeletedFinalStateUnknown{Key: "node-x", Obj: testNode("node-x", "1")})
	assert.Empty(t, results)

	require.NoError(t, indexer.Add(testNode("node-b", "2")))
	handler.OnAdd(testNode("node-b", "2"), false)
	require.Len(t, results, 1)
	assert.Equal(t, 2, (<-results).Values["nodeCount"])
}
