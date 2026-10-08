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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"

	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/mia-platform/ibdm/internal/source"
)

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
	assert.Equal(t, swType, knownTypes[len(knownTypes)-1].name)
	assert.Equal(t, workloadHelmReleaseRelationshipType, knownTypes[len(knownTypes)-2].name)
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
