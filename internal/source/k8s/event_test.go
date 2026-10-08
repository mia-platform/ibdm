// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package k8s

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	fakediscovery "k8s.io/client-go/discovery/fake"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"

	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/mia-platform/ibdm/internal/source"
)

// eventTimeout bounds every wait on the stream, so that a regression fails instead of hanging.
const eventTimeout = 10 * time.Second

var (
	namespacesGVR   = corev1.SchemeGroupVersion.WithResource("namespaces")
	servicesGVR     = corev1.SchemeGroupVersion.WithResource("services")
	secretsGVR      = corev1.SchemeGroupVersion.WithResource("secrets")
	deploymentsGVR  = appsv1.SchemeGroupVersion.WithResource("deployments")
	statefulSetsGVR = appsv1.SchemeGroupVersion.WithResource("statefulsets")
	daemonSetsGVR   = appsv1.SchemeGroupVersion.WithResource("daemonsets")
)

// streamHarness runs StartEventStream against a fake clientset.
type streamHarness struct {
	t       *testing.T
	client  *fake.Clientset
	dynamic *dynamicfake.FakeDynamicClient
	results chan source.Data
	watches chan string
	done    chan error
	cancel  context.CancelFunc
	stop    sync.Once

	mu            sync.Mutex
	secretListing []string
}

// startStream starts the event stream for types and returns once every informer has
// established its watch, so that changes made afterwards cannot be missed.
func startStream(t *testing.T, types []string, objects ...runtime.Object) *streamHarness {
	t.Helper()
	return startStreamWithCRDs(t, types, nil, nil, objects...)
}

// startStreamWithCRDs is startStream with a fake discovery set up by configure (nil
// serves nothing) and a fake dynamic client preloaded with crdObjects.
func startStreamWithCRDs(t *testing.T, types []string, configure func(*fakediscovery.FakeDiscovery), crdObjects []runtime.Object, objects ...runtime.Object) *streamHarness {
	t.Helper()
	setupFixedTime(t)

	harness := &streamHarness{
		t:       t,
		client:  fake.NewClientset(objects...),
		results: make(chan source.Data, 100),
		watches: make(chan string, 100),
		done:    make(chan error, 1),
	}
	harness.client.PrependWatchReactor("*", func(action k8stesting.Action) (bool, watch.Interface, error) {
		watcher, err := harness.client.Tracker().Watch(action.GetResource(), action.GetNamespace())
		if err != nil {
			return false, nil, err
		}
		harness.watches <- action.GetResource().Resource
		return true, watcher, nil
	})
	discovery, ok := harness.client.Discovery().(*fakediscovery.FakeDiscovery)
	require.True(t, ok)
	if configure != nil {
		configure(discovery)
	}

	harness.dynamic = dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		traefikV1Alpha1: "IngressRouteList",
		certManagerV1:   "CertificateList",
	}, crdObjects...)
	harness.dynamic.PrependWatchReactor("*", func(action k8stesting.Action) (bool, watch.Interface, error) {
		watcher, err := harness.dynamic.Tracker().Watch(action.GetResource(), action.GetNamespace())
		if err != nil {
			return false, nil, err
		}
		harness.watches <- action.GetResource().Resource
		return true, watcher, nil
	})
	harness.client.PrependReactor("list", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		harness.mu.Lock()
		defer harness.mu.Unlock()
		harness.secretListing = append(harness.secretListing, action.(k8stesting.ListAction).GetListRestrictions().Labels.String())
		return false, nil, nil
	})

	src := &Source{apiServer: testAPIServer, clusterName: testClusterName, clientset: harness.client, dynamic: harness.dynamic}
	extras := make(map[string]source.Extra, len(types))
	for _, name := range types {
		extras[name] = nil
	}

	ctx, cancel := context.WithCancel(t.Context())
	harness.cancel = cancel
	go func() { harness.done <- src.StartEventStream(ctx, extras, harness.results) }()
	t.Cleanup(harness.shutdown)
	return harness
}

// nilLogger returns the logger carried by the test context.
func nilLogger(t *testing.T) logger.Logger {
	t.Helper()
	return logger.FromContext(t.Context())
}

// waitWatches blocks until count watches have been established.
func (h *streamHarness) waitWatches(count int) {
	h.t.Helper()
	for range count {
		select {
		case <-h.watches:
		case <-time.After(eventTimeout):
			h.t.Fatal("timed out waiting for the informers to start watching")
		}
	}
}

// next returns the next emitted data.
func (h *streamHarness) next() source.Data {
	h.t.Helper()
	select {
	case data := <-h.results:
		return data
	case <-time.After(eventTimeout):
		h.t.Fatal("timed out waiting for data")
		return source.Data{}
	}
}

// nextN returns the next count emitted data items.
func (h *streamHarness) nextN(count int) []source.Data {
	h.t.Helper()
	items := make([]source.Data, 0, count)
	for range count {
		items = append(items, h.next())
	}
	return items
}

// shutdown cancels the stream and checks that it returns nil.
func (h *streamHarness) shutdown() {
	h.stop.Do(func() {
		h.cancel()
		select {
		case err := <-h.done:
			assert.NoError(h.t, err)
		case <-time.After(eventTimeout):
			h.t.Error("timed out waiting for the stream to stop")
		}
	})
}

func (h *streamHarness) create(gvr schema.GroupVersionResource, obj runtime.Object, namespace string) {
	h.t.Helper()
	require.NoError(h.t, h.client.Tracker().Create(gvr, obj, namespace))
}

func (h *streamHarness) update(gvr schema.GroupVersionResource, obj runtime.Object, namespace string) {
	h.t.Helper()
	require.NoError(h.t, h.client.Tracker().Update(gvr, obj, namespace))
}

func (h *streamHarness) remove(gvr schema.GroupVersionResource, namespace, name string) {
	h.t.Helper()
	require.NoError(h.t, h.client.Tracker().Delete(gvr, namespace, name))
}

func helmSecret(t *testing.T, namespace, name string, revision int, status string) *corev1.Secret {
	t.Helper()
	release := testRelease(namespace, name, revision)
	return newHelmSecret(namespace, name, revision, status, helmPayload(t, release, true))
}

func TestStartEventStreamInitialSnapshot(t *testing.T) {
	harness := startStream(t,
		[]string{namespaceType, deploymentType, statefulSetType, daemonSetType, serviceType, helmReleaseType, clusterType, ingressRouteType, certificateType, "unknown"},
		newNamespace("team-a", map[string]string{"env": "test"}),
		newDeployment("team-a", "web", nil, int32Ptr(2), testPodSpec([]corev1.Container{testContainer("app", "img:1")}, nil)),
		newStatefulSet("team-a", "db", nil, nil, testPodSpec(nil, nil)),
		newDaemonSet("team-a", "agent", nil, testPodSpec(nil, nil)),
		newService("team-a", "web-svc", nil, corev1.ServiceSpec{}),
		helmSecret(t, "team-a", "shop", 3, "deployed"),
		helmSecret(t, "team-a", "shop", 2, "superseded"),
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "plain"}},
	)

	byType := make(map[string]source.Data)
	for _, data := range harness.nextN(7) {
		assert.Equal(t, source.DataOperationUpsert, data.Operation)
		assert.Equal(t, testFixedTime, data.Time)
		byType[data.Type] = data
	}
	require.Len(t, byType, 7)

	namespace := byType[namespaceType].Values
	assert.Equal(t, testAPIServer, namespace[keyAPIServer])
	assert.Equal(t, "team-a", namespace[keyNamespace].(map[string]any)["metadata"].(map[string]any)["name"])
	assert.Equal(t, 2, byType[deploymentType].Values["replicas"])
	assert.Equal(t, "web", byType[deploymentType].Values[keyName])
	assert.Equal(t, "db", byType[statefulSetType].Values[keyName])
	assert.Equal(t, "agent", byType[daemonSetType].Values[keyName])
	assert.Equal(t, "web-svc", byType[serviceType].Values[keyName])
	assert.Equal(t, "shop", byType[helmReleaseType].Values[keyName])
	assert.Equal(t, 3, byType[helmReleaseType].Values["revision"])

	assert.Equal(t, 0, byType[clusterType].Values["nodeCount"])
	assert.Equal(t, testClusterName, byType[clusterType].Values["clusterName"])

	harness.waitWatches(7)
	harness.mu.Lock()
	defer harness.mu.Unlock()
	assert.Equal(t, []string{helmSecretSelector}, harness.secretListing[:1])
}

func TestStartEventStreamLiveChanges(t *testing.T) {
	pod := testPodSpec([]corev1.Container{testContainer("app", "img:1")}, nil)

	versioned := func(meta metav1.ObjectMeta, version string) metav1.ObjectMeta {
		meta.ResourceVersion = version
		return meta
	}

	tests := map[string]struct {
		dataType string
		gvr      schema.GroupVersionResource
		ns       string
		build    func(version string) runtime.Object
	}{
		"namespace": {
			dataType: namespaceType, gvr: namespacesGVR,
			build: func(version string) runtime.Object {
				return &corev1.Namespace{ObjectMeta: versioned(testMeta("", "team-a", map[string]string{"v": version}), version)}
			},
		},
		"deployment": {
			dataType: deploymentType, gvr: deploymentsGVR, ns: "team-a",
			build: func(version string) runtime.Object {
				obj := newDeployment("team-a", "web", map[string]string{"v": version}, nil, pod)
				obj.ResourceVersion = version
				return obj
			},
		},
		"statefulset": {
			dataType: statefulSetType, gvr: statefulSetsGVR, ns: "team-a",
			build: func(version string) runtime.Object {
				obj := newStatefulSet("team-a", "db", map[string]string{"v": version}, nil, pod)
				obj.ResourceVersion = version
				return obj
			},
		},
		"daemonset": {
			dataType: daemonSetType, gvr: daemonSetsGVR, ns: "team-a",
			build: func(version string) runtime.Object {
				obj := newDaemonSet("team-a", "agent", map[string]string{"v": version}, pod)
				obj.ResourceVersion = version
				return obj
			},
		},
		"service": {
			dataType: serviceType, gvr: servicesGVR, ns: "team-a",
			build: func(version string) runtime.Object {
				obj := newService("team-a", "web-svc", map[string]string{"v": version}, corev1.ServiceSpec{})
				obj.ResourceVersion = version
				return obj
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			harness := startStream(t, []string{test.dataType})
			harness.waitWatches(1)

			kind := findEventKind(t, test.dataType)
			created, updated := test.build("1"), test.build("2")

			harness.create(test.gvr, created, test.ns)
			data := harness.next()
			wantCreated, err := kind.values(created, testAPIServer)
			require.NoError(t, err)
			assert.Equal(t, source.Data{Type: test.dataType, Operation: source.DataOperationUpsert, Values: wantCreated, Time: testFixedTime}, data)

			harness.update(test.gvr, updated, test.ns)
			data = harness.next()
			wantUpdated, err := kind.values(updated, testAPIServer)
			require.NoError(t, err)
			assert.Equal(t, source.DataOperationUpsert, data.Operation)
			assert.Equal(t, wantUpdated, data.Values)
			assert.NotEqual(t, wantCreated, data.Values)

			harness.remove(test.gvr, test.ns, created.(metav1.Object).GetName())
			data = harness.next()
			assert.Equal(t, source.DataOperationDelete, data.Operation)
			assert.Equal(t, test.dataType, data.Type)
			assert.Equal(t, wantUpdated, data.Values)
		})
	}
}

func findEventKind(t *testing.T, name string) eventKind {
	t.Helper()
	for _, kind := range eventKinds {
		if kind.name == name {
			return kind
		}
	}
	t.Fatalf("event kind %q not found", name)
	return eventKind{}
}

func TestStartEventStreamOnlyRequestedTypes(t *testing.T) {
	harness := startStream(t, []string{deploymentType},
		newNamespace("team-a", nil),
		newDeployment("team-a", "web", nil, nil, testPodSpec(nil, nil)),
	)
	harness.waitWatches(1)

	harness.create(namespacesGVR, newNamespace("team-b", nil), "")
	harness.create(deploymentsGVR, newDeployment("team-a", "api", nil, nil, testPodSpec(nil, nil)), "team-a")

	names := []any{harness.next().Values[keyName], harness.next().Values[keyName]}
	assert.ElementsMatch(t, []any{"web", "api"}, names)
	assert.Empty(t, harness.results)
}

func TestStartEventStreamUnsupportedTypesOnly(t *testing.T) {
	setupFixedTime(t)
	s := newFakeSource(t, newNamespace("team-a", nil))
	results := make(chan source.Data, 1)

	err := s.StartEventStream(t.Context(), map[string]source.Extra{"unknown": nil, "node": nil}, results)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestStartEventStreamHelmUpgradeEmitsNoDelete(t *testing.T) {
	harness := startStream(t, []string{helmReleaseType}, helmSecret(t, "team-a", "shop", 1, "deployed"))
	first := harness.next()
	assert.Equal(t, 1, first.Values["revision"])
	harness.waitWatches(1)

	// helm upgrade: the new revision is deployed and the old one becomes superseded.
	harness.create(secretsGVR, helmSecret(t, "team-a", "shop", 2, "deployed"), "team-a")
	upgraded := harness.next()
	assert.Equal(t, source.DataOperationUpsert, upgraded.Operation)
	assert.Equal(t, 2, upgraded.Values["revision"])

	harness.update(secretsGVR, helmSecret(t, "team-a", "shop", 1, "superseded"), "team-a")

	// the sentinel release proves nothing was emitted for the superseded revision.
	harness.create(secretsGVR, helmSecret(t, "team-a", "sentinel", 1, "deployed"), "team-a")
	sentinel := harness.next()
	assert.Equal(t, source.DataOperationUpsert, sentinel.Operation)
	assert.Equal(t, "sentinel", sentinel.Values[keyName])

	// the real server reports the leave as a delete of the old revision.
	harness.remove(secretsGVR, "team-a", "sh.helm.release.v1.shop.v1")
	harness.create(secretsGVR, helmSecret(t, "team-a", "sentinel-two", 1, "deployed"), "team-a")
	assert.Equal(t, "sentinel-two", harness.next().Values[keyName])
}

func TestStartEventStreamHelmUninstallEmitsDelete(t *testing.T) {
	harness := startStream(t, []string{helmReleaseType},
		helmSecret(t, "team-a", "shop", 1, "deployed"),
		helmSecret(t, "team-a", "blog", 4, "deployed"),
	)
	harness.nextN(2)
	harness.waitWatches(1)

	harness.remove(secretsGVR, "team-a", "sh.helm.release.v1.shop.v1")
	deleted := harness.next()
	assert.Equal(t, source.DataOperationDelete, deleted.Operation)
	assert.Equal(t, helmReleaseType, deleted.Type)
	assert.Equal(t, "shop", deleted.Values[keyName])
	assert.Equal(t, "team-a", deleted.Values[keyNamespace])
	assert.Equal(t, testAPIServer, deleted.Values[keyAPIServer])

	// uninstall that keeps the history: the Secret only leaves the deployed set.
	harness.update(secretsGVR, helmSecret(t, "team-a", "blog", 4, "uninstalled"), "team-a")
	deleted = harness.next()
	assert.Equal(t, source.DataOperationDelete, deleted.Operation)
	assert.Equal(t, "blog", deleted.Values[keyName])
}

func TestStartEventStreamHelmIgnoresNonHelmSecrets(t *testing.T) {
	harness := startStream(t, []string{helmReleaseType},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "plain"}},
	)
	harness.waitWatches(1)

	harness.create(secretsGVR, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "other"}}, "team-a")
	failed := helmSecret(t, "team-a", "broken", 1, "failed")
	harness.create(secretsGVR, failed, "team-a")
	harness.update(secretsGVR, failed, "team-a")
	harness.remove(secretsGVR, "team-a", "plain")
	harness.remove(secretsGVR, "team-a", "other")
	harness.create(secretsGVR, helmSecret(t, "team-a", "sentinel", 1, "deployed"), "team-a")

	data := harness.next()
	assert.Equal(t, source.DataOperationUpsert, data.Operation)
	assert.Equal(t, "sentinel", data.Values[keyName])
}

func TestStartEventStreamHelmUndecodableSecretIgnored(t *testing.T) {
	broken := newHelmSecret("team-a", "broken", 1, "deployed", []byte("not base64 !!"))
	harness := startStream(t, []string{helmReleaseType}, broken)
	harness.waitWatches(1)

	harness.remove(secretsGVR, "team-a", broken.Name)
	harness.create(secretsGVR, helmSecret(t, "team-a", "sentinel", 1, "deployed"), "team-a")
	assert.Equal(t, "sentinel", harness.next().Values[keyName])
}

func TestStartEventStreamStopsOnCancellation(t *testing.T) {
	harness := startStream(t, []string{namespaceType, helmReleaseType})
	harness.waitWatches(2)

	harness.cancel()
	select {
	case err := <-harness.done:
		assert.NoError(t, err)
	case <-time.After(eventTimeout):
		t.Fatal("timed out waiting for the stream to stop")
	}
	harness.stop.Do(func() {})
}

func TestStartEventStreamStopsWhileSending(t *testing.T) {
	setupFixedTime(t)
	s := newFakeSource(t, newNamespace("team-a", nil), newNamespace("team-b", nil))
	results := make(chan source.Data) // never read: the handlers stay blocked in send

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- s.StartEventStream(ctx, map[string]source.Extra{namespaceType: nil}, results) }()

	cancel()
	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(eventTimeout):
		t.Fatal("timed out waiting for the stream to stop")
	}
}

func TestStartEventStreamInformerFailureDoesNotBlockOthers(t *testing.T) {
	previous := informerSyncTimeout
	informerSyncTimeout = 50 * time.Millisecond
	t.Cleanup(func() { informerSyncTimeout = previous })
	setupFixedTime(t)

	client := fake.NewClientset(newNamespace("team-a", nil))
	client.PrependReactor("list", "deployments", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("forbidden")
	})
	s := &Source{apiServer: testAPIServer, clientset: client}
	results := make(chan source.Data, 10)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- s.StartEventStream(ctx, map[string]source.Extra{deploymentType: nil, namespaceType: nil}, results)
	}()

	select {
	case data := <-results:
		assert.Equal(t, namespaceType, data.Type)
	case <-time.After(eventTimeout):
		t.Fatal("timed out waiting for the healthy informer")
	}

	// let the failing informer outlive its sync timeout before stopping.
	time.Sleep(3 * informerSyncTimeout)
	cancel()
	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(eventTimeout):
		t.Fatal("timed out waiting for the stream to stop")
	}
}

func TestEventHandlersUnchangedResourceVersion(t *testing.T) {
	setupFixedTime(t)
	results := make(chan source.Data, 10)
	stream := &eventStream{ctx: t.Context(), log: nilLogger(t), apiServer: testAPIServer, results: results}
	handler := stream.handlers(findEventKind(t, deploymentType))

	pod := testPodSpec(nil, nil)
	old := newDeployment("team-a", "web", nil, nil, pod)
	old.ResourceVersion = "7"
	same := newDeployment("team-a", "web", map[string]string{"x": "y"}, nil, pod)
	same.ResourceVersion = "7"
	changed := newDeployment("team-a", "web", nil, int32Ptr(3), pod)
	changed.ResourceVersion = "8"

	handler.OnUpdate(old, same)
	assert.Empty(t, results)

	handler.OnUpdate(old, changed)
	require.Len(t, results, 1)
	assert.Equal(t, 3, (<-results).Values["replicas"])

	// objects without a resource version cannot be proven unchanged.
	old.ResourceVersion, same.ResourceVersion = "", ""
	handler.OnUpdate(old, same)
	assert.Len(t, results, 1)
}

func TestEventHandlersTombstoneDelete(t *testing.T) {
	setupFixedTime(t)
	results := make(chan source.Data, 10)
	stream := &eventStream{ctx: t.Context(), log: nilLogger(t), apiServer: testAPIServer, results: results}
	handler := stream.handlers(findEventKind(t, deploymentType))

	deployment := newDeployment("team-a", "web", nil, nil, testPodSpec(nil, nil))
	handler.OnDelete(cache.DeletedFinalStateUnknown{Key: "team-a/web", Obj: deployment})
	require.Len(t, results, 1)
	data := <-results
	assert.Equal(t, source.DataOperationDelete, data.Operation)
	assert.Equal(t, "web", data.Values[keyName])
	assert.Equal(t, "team-a", data.Values[keyNamespace])

	// a tombstone holding something unexpected is logged and dropped.
	handler.OnDelete(cache.DeletedFinalStateUnknown{Key: "team-a/web", Obj: "garbage"})
	handler.OnAdd(&corev1.Pod{}, false)
	assert.Empty(t, results)
}

func TestEventHandlersHelmTombstoneDelete(t *testing.T) {
	results := make(chan source.Data, 10)
	stream := &eventStream{ctx: t.Context(), log: nilLogger(t), apiServer: testAPIServer, results: results}
	informer := informers.NewSharedInformerFactory(fake.NewClientset(), 0).Core().V1().Secrets().Informer()
	handler := stream.helmHandlers(informer)

	handler.OnDelete(cache.DeletedFinalStateUnknown{Key: "team-a/x", Obj: helmSecret(t, "team-a", "shop", 1, "deployed")})
	require.Len(t, results, 1)
	data := <-results
	assert.Equal(t, source.DataOperationDelete, data.Operation)
	assert.Equal(t, "shop", data.Values[keyName])

	handler.OnDelete(cache.DeletedFinalStateUnknown{Key: "team-a/x", Obj: "garbage"})
	handler.OnAdd("garbage", false)
	handler.OnUpdate("garbage", "garbage")
	assert.Empty(t, results)
}

// newSecretIndexer builds the indexer of a Secrets informer holding secrets.
func newSecretIndexer(t *testing.T, secrets ...*corev1.Secret) cache.Indexer {
	t.Helper()
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	for _, secret := range secrets {
		require.NoError(t, indexer.Add(secret))
	}
	return indexer
}

func TestHelmReleaseRevisionSelection(t *testing.T) {
	tests := map[string]struct {
		cached    []*corev1.Secret
		gone      *corev1.Secret
		upsert    *corev1.Secret
		wantOps   []source.DataOperation
		wantRevOf int
	}{
		"deleted lower revision while a higher one remains emits nothing": {
			cached: []*corev1.Secret{helmSecret(t, "team-a", "shop", 3, "deployed")},
			gone:   helmSecret(t, "team-a", "shop", 2, "superseded"),
		},
		"deleted highest revision while a lower one remains upserts the remaining": {
			cached:    []*corev1.Secret{helmSecret(t, "team-a", "shop", 1, "deployed"), helmSecret(t, "team-a", "shop", 2, "deployed")},
			gone:      helmSecret(t, "team-a", "shop", 3, "superseded"),
			wantOps:   []source.DataOperation{source.DataOperationUpsert},
			wantRevOf: 2,
		},
		"other releases and namespaces do not keep the release alive": {
			cached: []*corev1.Secret{
				helmSecret(t, "team-a", "blog", 5, "deployed"),
				helmSecret(t, "team-b", "shop", 5, "deployed"),
				helmSecret(t, "team-a", "shop", 6, "superseded"),
			},
			gone:      helmSecret(t, "team-a", "shop", 2, "deployed"),
			wantOps:   []source.DataOperation{source.DataOperationDelete},
			wantRevOf: 2,
		},
		"upsert is skipped when a higher deployed revision exists": {
			cached: []*corev1.Secret{helmSecret(t, "team-a", "shop", 2, "deployed")},
			upsert: helmSecret(t, "team-a", "shop", 1, "deployed"),
		},
		"upsert of the highest revision is emitted": {
			cached:    []*corev1.Secret{helmSecret(t, "team-a", "shop", 1, "deployed")},
			upsert:    helmSecret(t, "team-a", "shop", 2, "deployed"),
			wantOps:   []source.DataOperation{source.DataOperationUpsert},
			wantRevOf: 2,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			setupFixedTime(t)
			results := make(chan source.Data, 10)
			stream := &eventStream{ctx: t.Context(), log: nilLogger(t), apiServer: testAPIServer, results: results}
			indexer := newSecretIndexer(t, test.cached...)

			if test.gone != nil {
				stream.helmGone(indexer, test.gone)
			}
			if test.upsert != nil {
				stream.helmUpsert(indexer, test.upsert)
			}

			close(results)
			items := collectData(results)
			require.Len(t, items, len(test.wantOps))
			for i, item := range items {
				assert.Equal(t, test.wantOps[i], item.Operation)
				assert.Equal(t, test.wantRevOf, item.Values["revision"])
			}
		})
	}
}

func TestHelmSecretRevision(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 12, helmSecretRevision(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{helmVersionLabel: strconv.Itoa(12)}}}))
	assert.Equal(t, 0, helmSecretRevision(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{helmVersionLabel: "x"}}}))
	assert.Equal(t, 0, helmSecretRevision(&corev1.Secret{}))
}

func TestSameResourceVersionNonObjects(t *testing.T) {
	t.Parallel()

	assert.False(t, sameResourceVersion("a", "b"))
	assert.False(t, sameResourceVersion(&corev1.Secret{}, "b"))
}

func TestRegisterFailsOnStartedInformer(t *testing.T) {
	setupFixedTime(t)
	factory := informers.NewSharedInformerFactory(fake.NewClientset(), 0)
	informer := factory.Core().V1().Namespaces().Informer()
	stream := &eventStream{ctx: t.Context(), log: nilLogger(t), results: make(chan source.Data, 1)}

	ctx, cancel := context.WithCancel(t.Context())
	factory.StartWithContext(ctx)
	require.True(t, cache.WaitForCacheSync(ctx.Done(), informer.HasSynced))

	assert.False(t, stream.register(namespaceType, informer, cache.ResourceEventHandlerFuncs{}))
	cancel()
	factory.Shutdown()
}

func TestTypedValuesUnexpectedObject(t *testing.T) {
	t.Parallel()

	_, err := typedValues(infallible(deploymentValues))(&corev1.Pod{}, testAPIServer)
	require.ErrorIs(t, err, errUnexpectedObject)
}

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

var podsGVR = corev1.SchemeGroupVersion.WithResource("pods")

// podAt returns a copy of pod with the given resource version, after applying mutate.
func podAt(pod *corev1.Pod, version string, mutate ...func(*corev1.Pod)) *corev1.Pod {
	copied := pod.DeepCopy()
	copied.ResourceVersion = version
	for _, fn := range mutate {
		fn(copied)
	}
	return copied
}

func withAnnotation(key, value string) func(*corev1.Pod) {
	return func(pod *corev1.Pod) {
		pod.Annotations = map[string]string{key: value}
	}
}

func onNode1(pod *corev1.Pod) { pod.Spec.NodeName = "node-1" }

func withPodIP(ip string) func(*corev1.Pod) {
	return func(pod *corev1.Pod) { pod.Status.PodIP = ip }
}

// withContainer sets the single container of the pod, with its status.
func withContainer(image, imageID string, ready bool, restarts int32, state corev1.ContainerState) func(*corev1.Pod) {
	return func(pod *corev1.Pod) {
		pod.Spec.Containers = []corev1.Container{testContainer("app", image)}
		pod.Status.ContainerStatuses = []corev1.ContainerStatus{{
			Name: "app", Image: image, ImageID: imageID, Ready: ready, RestartCount: restarts, State: state,
		}}
	}
}

// withStatusNoise changes only fields that are not part of the emitted values.
func withStatusNoise(ready bool, restarts int32, state corev1.ContainerState) func(*corev1.Pod) {
	return func(pod *corev1.Pod) {
		pod.Status.Conditions = append(pod.Status.Conditions, corev1.PodCondition{Type: corev1.PodReady, Status: corev1.ConditionTrue, Reason: "r" + strconv.Itoa(int(restarts))})
		pod.Status.ContainerStatuses[0].Ready = ready
		pod.Status.ContainerStatuses[0].RestartCount = restarts
		pod.Status.ContainerStatuses[0].State = state
	}
}

func podItem(t *testing.T, operation source.DataOperation, pod *corev1.Pod) source.Data {
	t.Helper()
	return source.Data{Type: podType, Operation: operation, Values: podValues(pod, testAPIServer), Time: testFixedTime}
}

func ownedPod(name string, mutate ...func(*corev1.Pod)) *corev1.Pod {
	return newPod("team-a", name, append([]func(*corev1.Pod){ownedBy("StatefulSet", "db", boolPtr(true))}, mutate...)...)
}

func TestPodIsStreamable(t *testing.T) {
	t.Parallel()

	for _, name := range []string{podType, podOwnerRelationshipType} {
		assert.True(t, isKnownType(name), name)
		assert.True(t, isStreamable(name), name)
	}
	assert.NotNil(t, findEventKind(t, podType).excluded)
	for _, kind := range eventKinds {
		if kind.name != podType {
			assert.Nil(t, kind.excluded, kind.name)
		}
	}
}

func TestStartEventStreamPodSnapshotSkipsSucceeded(t *testing.T) {
	running := newPod("team-a", "running", withPhase(corev1.PodRunning))
	pending := newPod("team-a", "pending", withPhase(corev1.PodPending))
	failed := newPod("team-a", "failed", withPhase(corev1.PodFailed))
	done := newPod("team-a", "done", withPhase(corev1.PodSucceeded))

	harness := startStream(t, []string{podType}, running, pending, failed, done)
	assert.ElementsMatch(t, []source.Data{
		podItem(t, source.DataOperationUpsert, running),
		podItem(t, source.DataOperationUpsert, pending),
		podItem(t, source.DataOperationUpsert, failed),
	}, harness.nextN(3))
	harness.waitWatches(1)
	assert.Empty(t, harness.results)
}

func TestStartEventStreamPodLiveChanges(t *testing.T) {
	base := newPod("team-a", "web-0", withPhase(corev1.PodPending), withContainer("img:1", "id:1", false, 0, corev1.ContainerState{}))
	base.ResourceVersion = "1"

	harness := startStream(t, []string{podType})
	harness.waitWatches(1)

	// create
	harness.create(podsGVR, base, "team-a")
	assert.Equal(t, podItem(t, source.DataOperationUpsert, base), harness.next())

	// real changes emit exactly one upsert each, interleaved with status only updates
	// that emit nothing: the next item received is always the real change.
	running := corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}
	current := base
	version := 1
	apply := func(mutate ...func(*corev1.Pod)) *corev1.Pod {
		version++
		current = podAt(current, strconv.Itoa(version), mutate...)
		harness.update(podsGVR, current, "team-a")
		return current
	}

	realChanges := map[string]func(*corev1.Pod){
		"annotation": withAnnotation("note", "x"),
		"label":      func(pod *corev1.Pod) { pod.Labels = map[string]string{"app": "web"} },
		"nodeName":   onNode1,
		"phase":      withPhase(corev1.PodRunning),
		"podIP":      withPodIP("10.0.0.7"),
		"imageID":    withContainer("img:1", "id:2", false, 0, corev1.ContainerState{}),
	}
	for _, name := range []string{"annotation", "label", "nodeName", "phase", "podIP", "imageID"} {
		// status only updates: conditions, ready, restartCount and state
		apply(withStatusNoise(true, 1, running))
		apply(withStatusNoise(false, 2, corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 1}}))

		want := apply(realChanges[name])
		assert.Equal(t, podItem(t, source.DataOperationUpsert, want), harness.next(), name)
	}

	// delete carries the last known values
	harness.remove(podsGVR, "team-a", "web-0")
	assert.Equal(t, podItem(t, source.DataOperationDelete, current), harness.next())
	assert.Empty(t, harness.results)
}

func TestStartEventStreamPodCompletion(t *testing.T) {
	running := newPod("team-a", "job-0", withPhase(corev1.PodRunning))
	running.ResourceVersion = "1"
	other := newPod("team-a", "other", withPhase(corev1.PodRunning))
	done := newPod("team-a", "old-done", withPhase(corev1.PodSucceeded))
	done.ResourceVersion = "1"

	harness := startStream(t, []string{podType}, running, other, done)
	harness.nextN(2)
	harness.waitWatches(1)

	// Running -> Succeeded deletes the item with the values of the previous object.
	completed := podAt(running, "2", withPhase(corev1.PodSucceeded))
	harness.update(podsGVR, completed, "team-a")
	assert.Equal(t, podItem(t, source.DataOperationDelete, running), harness.next())

	// Further updates and the deletion of a completed pod emit nothing; the deletion
	// of an already completed pod present in the snapshot as well.
	harness.update(podsGVR, podAt(completed, "3", withAnnotation("k", "v")), "team-a")
	harness.remove(podsGVR, "team-a", "job-0")
	harness.remove(podsGVR, "team-a", "old-done")

	// A Succeeded pod created later is ignored.
	harness.create(podsGVR, newPod("team-a", "late-done", withPhase(corev1.PodSucceeded)), "team-a")

	// The sentinel proves that nothing was emitted before it.
	sentinel := newPod("team-a", "sentinel", withPhase(corev1.PodRunning))
	harness.create(podsGVR, sentinel, "team-a")
	assert.Equal(t, podItem(t, source.DataOperationUpsert, sentinel), harness.next())

	// A completed pod that becomes not completed again is upserted.
	harness.update(podsGVR, podAt(other, "5", withPhase(corev1.PodSucceeded)), "team-a")
	assert.Equal(t, podItem(t, source.DataOperationDelete, other), harness.next())
	revived := podAt(other, "6", withPhase(corev1.PodRunning))
	harness.update(podsGVR, revived, "team-a")
	assert.Equal(t, podItem(t, source.DataOperationUpsert, revived), harness.next())
	assert.Empty(t, harness.results)
}

func TestEventHandlersPodExcluded(t *testing.T) {
	setupFixedTime(t)
	results := make(chan source.Data, 10)
	stream := &eventStream{ctx: t.Context(), log: nilLogger(t), apiServer: testAPIServer, results: results}
	handler := stream.handlers(findEventKind(t, podType))

	active := podAt(newPod("team-a", "web-0", withPhase(corev1.PodRunning)), "1")
	completed := podAt(active, "2", withPhase(corev1.PodSucceeded))
	completedAgain := podAt(completed, "3", withAnnotation("k", "v"))
	revived := podAt(completed, "4", withPhase(corev1.PodRunning))

	handler.OnAdd(completed, false)
	handler.OnUpdate(completed, completedAgain)
	handler.OnDelete(completed)
	handler.OnDelete(cache.DeletedFinalStateUnknown{Key: "team-a/web-0", Obj: completed})
	assert.Empty(t, results)

	handler.OnUpdate(active, completed)
	assert.Equal(t, podItem(t, source.DataOperationDelete, active), <-results)

	handler.OnUpdate(completed, revived)
	assert.Equal(t, podItem(t, source.DataOperationUpsert, revived), <-results)

	// tombstone of a live pod is a delete with its values.
	handler.OnDelete(cache.DeletedFinalStateUnknown{Key: "team-a/web-0", Obj: revived})
	assert.Equal(t, podItem(t, source.DataOperationDelete, revived), <-results)

	// the normal path keeps the resource version and no-op skips.
	handler.OnUpdate(active, podAt(active, "1", onNode1))
	handler.OnUpdate(active, podAt(active, "9"))
	assert.Empty(t, results)
	handler.OnUpdate(active, podAt(active, "9", onNode1))
	assert.Len(t, results, 1)
}

func TestIsCompletedPod(t *testing.T) {
	t.Parallel()

	assert.True(t, isCompletedPod(newPod("n", "p", withPhase(corev1.PodSucceeded))))
	assert.False(t, isCompletedPod(newPod("n", "p", withPhase(corev1.PodFailed))))
	assert.False(t, isCompletedPod(newPod("n", "p")))
	assert.False(t, isCompletedPod((*corev1.Pod)(nil)))
	assert.False(t, isCompletedPod("not a pod"))
}

func TestStartEventStreamPodTombstoneDeleteOfLivePod(t *testing.T) {
	// covered at handler level by TestEventHandlersPodExcluded; here a stream deletion
	// of a pod seen in the snapshot yields its delete.
	pod := newPod("team-a", "web-0", withPhase(corev1.PodRunning))
	harness := startStream(t, []string{podType}, pod)
	harness.nextN(1)
	harness.waitWatches(1)

	harness.remove(podsGVR, "team-a", "web-0")
	assert.Equal(t, podItem(t, source.DataOperationDelete, pod), harness.next())
}

func TestStartEventStreamPodOwnerRelationships(t *testing.T) {
	const relType = podOwnerRelationshipType

	web := newPod("team-a", "web-abc-1", withPhase(corev1.PodRunning), withLabels("pod-template-hash", "abc"), ownedBy("ReplicaSet", "web-abc", boolPtr(true)))
	db := ownedPod("db-0", withPhase(corev1.PodRunning), withContainer("img:1", "id:1", false, 0, corev1.ContainerState{}))
	agent := newPod("team-b", "agent-x", withPhase(corev1.PodPending), ownedBy("DaemonSet", "agent", boolPtr(true)))
	node := newPod("team-a", "static", ownedBy("Node", "node-1", boolPtr(true)))
	job := newPod("team-a", "batch-1", ownedBy("Job", "batch", boolPtr(true)))
	bare := newPod("team-a", "bare")
	done := ownedPod("db-old", withPhase(corev1.PodSucceeded))

	t.Run("snapshot", func(t *testing.T) {
		harness := startStream(t, []string{relType}, web, db, agent, node, job, bare, done)

		assert.ElementsMatch(t, []source.Data{
			relationData(relType, source.DataOperationUpsert, podOwnerPair("team-a", "web-abc-1", "Deployment", "web")),
			relationData(relType, source.DataOperationUpsert, podOwnerPair("team-a", "db-0", "StatefulSet", "db")),
			relationData(relType, source.DataOperationUpsert, podOwnerPair("team-b", "agent-x", "DaemonSet", "agent")),
		}, harness.nextN(3))
		harness.waitWatches(1)
		assert.Empty(t, harness.results)
	})

	t.Run("add, status updates, completion and delete", func(t *testing.T) {
		harness := startStream(t, []string{relType})
		harness.waitWatches(1)
		pair := podOwnerPair("team-a", "db-0", "StatefulSet", "db")

		created := podAt(db, "1")
		harness.create(podsGVR, created, "team-a")
		assert.Equal(t, relationData(relType, source.DataOperationUpsert, pair), harness.next())

		// not emitting owners and succeeded pods
		harness.create(podsGVR, node, "team-a")
		harness.create(podsGVR, job, "team-a")
		harness.create(podsGVR, bare, "team-a")
		harness.create(podsGVR, done, "team-a")

		// status only and unrelated updates emit nothing
		running := corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}
		harness.update(podsGVR, podAt(created, "2", withStatusNoise(true, 3, running)), "team-a")
		harness.update(podsGVR, podAt(created, "3", onNode1, withAnnotation("k", "v")), "team-a")

		// completion deletes the link, then nothing more is emitted for the pod
		completed := podAt(created, "4", withPhase(corev1.PodSucceeded))
		harness.update(podsGVR, completed, "team-a")
		assert.Equal(t, relationData(relType, source.DataOperationDelete, pair), harness.next())
		harness.remove(podsGVR, "team-a", "db-0")

		// the pod that was never emitted before is the first emission after the above
		fresh := podAt(db, "1", func(pod *corev1.Pod) { pod.Name = "db-1" })
		harness.create(podsGVR, fresh, "team-a")
		assert.Equal(t, relationData(relType, source.DataOperationUpsert, podOwnerPair("team-a", "db-1", "StatefulSet", "db")), harness.next())

		harness.remove(podsGVR, "team-a", "db-1")
		assert.Equal(t, relationData(relType, source.DataOperationDelete, podOwnerPair("team-a", "db-1", "StatefulSet", "db")), harness.next())
		assert.Empty(t, harness.results)
	})

	t.Run("owner change", func(t *testing.T) {
		harness := startStream(t, []string{relType}, podAt(db, "1"))
		harness.nextN(1)
		harness.waitWatches(1)

		changed := podAt(db, "2", func(pod *corev1.Pod) {
			pod.OwnerReferences = []metav1.OwnerReference{{Kind: "DaemonSet", Name: "agent", Controller: boolPtr(true)}}
		})
		harness.update(podsGVR, changed, "team-a")
		assert.Equal(t, []source.Data{
			relationData(relType, source.DataOperationUpsert, podOwnerPair("team-a", "db-0", "DaemonSet", "agent")),
			relationData(relType, source.DataOperationDelete, podOwnerPair("team-a", "db-0", "StatefulSet", "db")),
		}, harness.nextN(2))
	})

	t.Run("tombstone", func(t *testing.T) {
		results := make(chan source.Data, 10)
		stream := &eventStream{ctx: t.Context(), log: nilLogger(t), apiServer: testAPIServer, results: results}
		handler := stream.relationHandlers(relationshipBindings[len(relationshipBindings)-1])
		setupFixedTime(t)

		handler.OnDelete(cache.DeletedFinalStateUnknown{Key: "team-a/db-0", Obj: db})
		require.Len(t, results, 1)
		assert.Equal(t, relationData(relType, source.DataOperationDelete, podOwnerPair("team-a", "db-0", "StatefulSet", "db")), <-results)

		handler.OnDelete(cache.DeletedFinalStateUnknown{Key: "x", Obj: done})
		handler.OnAdd(done, false)
		assert.Empty(t, results)
	})
}

func TestPodOwnerPairsIgnoreCompletedPods(t *testing.T) {
	t.Parallel()

	assert.Empty(t, podOwnerPairs(ownedPod("db-0", withPhase(corev1.PodSucceeded)), testAPIServer))
	assert.Len(t, podOwnerPairs(ownedPod("db-0", withPhase(corev1.PodFailed)), testAPIServer), 1)
}

func TestStartEventStreamPodRelationshipOnlyStartsPodInformer(t *testing.T) {
	harness := startStream(t, []string{podOwnerRelationshipType}, ownedPod("db-0"))
	harness.nextN(1)
	harness.waitWatches(1)

	for _, action := range harness.client.Actions() {
		assert.Equal(t, "pods", action.GetResource().Resource, action.GetVerb())
	}
	assert.Empty(t, harness.watches)
}

func TestStartEventStreamPodAndRelationshipShareInformer(t *testing.T) {
	pod := ownedPod("db-0", withPhase(corev1.PodRunning))
	harness := startStream(t, []string{podType, podOwnerRelationshipType}, pod)

	types := map[string]int{}
	for _, data := range harness.nextN(2) {
		types[data.Type]++
	}
	assert.Equal(t, map[string]int{podType: 1, podOwnerRelationshipType: 1}, types)
	harness.waitWatches(1)

	var lists, watches int
	for _, action := range harness.client.Actions() {
		if action.GetResource().Resource != "pods" {
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

	// a completion deletes both
	harness.update(podsGVR, podAt(pod, "2", withPhase(corev1.PodSucceeded)), "team-a")
	got := harness.nextN(2)
	types = map[string]int{}
	for _, data := range got {
		assert.Equal(t, source.DataOperationDelete, data.Operation)
		types[data.Type]++
	}
	assert.Equal(t, map[string]int{podType: 1, podOwnerRelationshipType: 1}, types)
}

func TestStartEventStreamPodStopsOnCancellation(t *testing.T) {
	harness := startStream(t, []string{podType})
	harness.waitWatches(1)
	harness.shutdown()
}
