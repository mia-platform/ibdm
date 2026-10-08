// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package k8s

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"

	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/mia-platform/ibdm/internal/source"
)

const (
	// informerResync disables the periodic resync of the informers: only real
	// changes reported by the API server produce events.
	informerResync time.Duration = 0

	// helmOwnerLabel and helmOwnerValue identify the Secrets owned by Helm.
	helmOwnerLabel = "owner"
	helmOwnerValue = "helm"

	// helmStatusLabel and helmStatusDeployed identify the current revision of a Helm release.
	helmStatusLabel    = "status"
	helmStatusDeployed = "deployed"
)

// informerSyncTimeout is the time after which an informer that has not completed
// its initial sync is reported as failing; it keeps retrying afterwards.
var informerSyncTimeout = time.Minute

var (
	_ source.EventSource = &Source{}

	// errUnexpectedObject reports an informer object of an unexpected type.
	errUnexpectedObject = errors.New("unexpected object type")
)

// valuesBuilder builds the values of a data item from an informer object.
type valuesBuilder func(obj any, apiServer string) (map[string]any, error)

// eventKind describes a data type streamed through an informer. Typed kinds carry
// the informer constructor of the unfiltered factory; CRD-backed kinds are built
// by crdKind.eventKind and use a dynamic informer instead.
type eventKind struct {
	name     string
	informer func(factory informers.SharedInformerFactory) cache.SharedIndexInformer
	values   valuesBuilder
	// comparable, when set, strips from the values the fields that change on every
	// update without carrying information, before old and new values are compared.
	comparable func(values map[string]any) map[string]any
	// excluded, when set, reports the objects that are not items: they are never
	// emitted, and an object becoming excluded is deleted.
	excluded func(obj any) bool
}

// eventKinds lists the data types streamed by plain typed informers. helmrelease,
// cluster and the CRD-backed types are handled separately.
var eventKinds = []eventKind{
	{
		name: namespaceType,
		informer: func(f informers.SharedInformerFactory) cache.SharedIndexInformer {
			return f.Core().V1().Namespaces().Informer()
		},
		values:     typedValues(namespaceValues),
		comparable: withoutNamespaceResourceVersion,
	},
	{
		name: deploymentType,
		informer: func(f informers.SharedInformerFactory) cache.SharedIndexInformer {
			return f.Apps().V1().Deployments().Informer()
		},
		values: typedValues(infallible(deploymentValues)),
	},
	{
		name: statefulSetType,
		informer: func(f informers.SharedInformerFactory) cache.SharedIndexInformer {
			return f.Apps().V1().StatefulSets().Informer()
		},
		values: typedValues(infallible(statefulSetValues)),
	},
	{
		name: daemonSetType,
		informer: func(f informers.SharedInformerFactory) cache.SharedIndexInformer {
			return f.Apps().V1().DaemonSets().Informer()
		},
		values: typedValues(infallible(daemonSetValues)),
	},
	{
		name: serviceType,
		informer: func(f informers.SharedInformerFactory) cache.SharedIndexInformer {
			return f.Core().V1().Services().Informer()
		},
		values: typedValues(infallible(serviceValues)),
	},
	{
		name: networkPolicyType,
		informer: func(f informers.SharedInformerFactory) cache.SharedIndexInformer {
			return f.Networking().V1().NetworkPolicies().Informer()
		},
		values: typedValues(infallible(networkPolicyValues)),
	},
	{
		name: podType,
		informer: func(f informers.SharedInformerFactory) cache.SharedIndexInformer {
			return f.Core().V1().Pods().Informer()
		},
		values:   typedValues(infallible(podValues)),
		excluded: isCompletedPod,
	},
}

// infallible adapts a builder that cannot fail to the fallible signature.
func infallible[T any](build func(*T, string) map[string]any) func(*T, string) (map[string]any, error) {
	return func(obj *T, apiServer string) (map[string]any, error) {
		return build(obj, apiServer), nil
	}
}

// typedValues adapts a builder of *T to a valuesBuilder, checking the object type.
func typedValues[T any](build func(*T, string) (map[string]any, error)) valuesBuilder {
	return func(obj any, apiServer string) (map[string]any, error) {
		typed, ok := obj.(*T)
		if !ok {
			return nil, fmt.Errorf("%w: %T", errUnexpectedObject, obj)
		}
		return build(typed, apiServer)
	}
}

// withoutNamespaceResourceVersion returns values without the resource version of
// the namespace, which changes on every update of the object, whatever the field.
// The input is not modified.
func withoutNamespaceResourceVersion(values map[string]any) map[string]any {
	namespace, ok := values[keyNamespace].(map[string]any)
	if !ok {
		return values
	}
	metadata, ok := namespace["metadata"].(map[string]any)
	if !ok {
		return values
	}

	trimmedMetadata := maps.Clone(metadata)
	delete(trimmedMetadata, "resourceVersion")
	trimmedNamespace := maps.Clone(namespace)
	trimmedNamespace["metadata"] = trimmedMetadata
	trimmed := maps.Clone(values)
	trimmed[keyNamespace] = trimmedNamespace
	return trimmed
}

// isStreamable reports whether name is a data type supported in watch mode.
func isStreamable(name string) bool {
	if name == helmReleaseType || name == clusterType || name == serviceWorkloadRelationshipType || isRelationshipType(name) {
		return true
	}
	return slices.ContainsFunc(eventKinds, func(kind eventKind) bool { return kind.name == name }) ||
		slices.ContainsFunc(crdKinds, func(kind crdKind) bool { return kind.dataType == name })
}

// runningInformer is an informer registered for streaming.
type runningInformer struct {
	name     string
	informer cache.SharedIndexInformer
	// onSynced, when set, runs once the informer has completed its initial sync.
	onSynced func()
}

// eventStream carries what the informer handlers need to emit data.
type eventStream struct {
	ctx         context.Context
	log         logger.Logger
	apiServer   string
	clusterName string
	results     chan<- source.Data
	// reconcilers emit the relationship types derived from several informers.
	reconcilers []*pairReconciler
}

// StartEventStream streams the changes of the requested resource types as they
// happen, using shared informers. It blocks until ctx is cancelled and returns nil
// on cancellation. The initial snapshot of every informer is delivered as upserts, so
// the objects that already exist are emitted first. Updates that do not change any
// emitted value are skipped. The cluster item is built from a node informer and is
// emitted once after the nodes synced, then only when its values change; it is never
// deleted. serviceWorkloadRelationship is emitted by a pairReconciler fed by the
// service and workload informers (started even when their item types are not
// requested): nothing until all have synced, then the full pair set once, then only
// the differences. The CRD-backed types (ingressroute, certificate) use dynamic informers; a
// CRD that is not installed is skipped. Unknown types are skipped with a debug log,
// and an informer that fails to start or sync does not prevent the others from running.
func (s *Source) StartEventStream(ctx context.Context, typesToStream map[string]source.Extra, results chan<- source.Data) error {
	log := logger.FromContext(ctx).WithName(loggerName)

	requested := make(map[string]struct{}, len(typesToStream))
	for _, name := range slices.Sorted(maps.Keys(typesToStream)) {
		if !isStreamable(name) {
			log.Debug("skipping unsupported type", "type", name)
			continue
		}
		requested[name] = struct{}{}
	}

	stream := &eventStream{ctx: ctx, log: log, apiServer: s.apiServer, clusterName: s.clusterName, results: results}
	factory := informers.NewSharedInformerFactory(s.clientset, informerResync)
	var active []runningInformer

	stream.reconcilers = stream.newReconcilers(requested)

	for _, kind := range eventKinds {
		handlers := stream.sourceHandlers(kind, requested)
		if len(handlers) == 0 {
			continue
		}
		// One informer per kind, shared by the item and the relationship handlers.
		informer := kind.informer(factory)
		if stream.register(kind.name, informer, handlers...) {
			active = append(active, runningInformer{name: kind.name, informer: informer, onSynced: stream.bindReconcilers(kind.name, informer)})
		}
	}

	if _, ok := requested[clusterType]; ok {
		informer := factory.Core().V1().Nodes().Informer()
		cluster := &clusterEmitter{stream: stream, indexer: informer.GetIndexer()}
		if stream.register(clusterType, informer, cluster.handlers()) {
			active = append(active, runningInformer{name: clusterType, informer: informer, onSynced: cluster.emitInitial})
		}
	}

	dynamicFactory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(s.dynamic, informerResync, metav1.NamespaceAll, nil)
	for _, kind := range crdKinds {
		handlers := stream.sourceHandlers(kind.eventKind(), requested)
		if len(handlers) == 0 {
			continue
		}
		if running, ok := s.crdInformer(stream, dynamicFactory, kind, handlers); ok {
			active = append(active, running)
		}
	}

	// Helm release Secrets are filtered server-side: only deployed Helm Secrets are transferred.
	helmFactory := informers.NewSharedInformerFactoryWithOptions(s.clientset, informerResync,
		informers.WithTweakListOptions(func(options *metav1.ListOptions) {
			options.LabelSelector = helmSecretSelector
		}),
	)
	if _, ok := requested[helmReleaseType]; ok {
		informer := helmFactory.Core().V1().Secrets().Informer()
		if stream.register(helmReleaseType, informer, stream.helmHandlers(informer)) {
			active = append(active, runningInformer{name: helmReleaseType, informer: informer})
		}
	}

	if len(active) == 0 {
		log.Warn("no supported type to stream")
		return nil
	}

	factory.StartWithContext(ctx)
	helmFactory.StartWithContext(ctx)
	dynamicFactory.Start(ctx.Done())

	var waiters sync.WaitGroup
	for _, running := range active {
		waiters.Go(func() { stream.waitForSync(running) })
	}

	<-ctx.Done()
	log.Debug("stopping informers")
	factory.Shutdown()
	helmFactory.Shutdown()
	dynamicFactory.Shutdown()
	waiters.Wait()
	return nil
}

// crdInformer resolves the GVR of kind through discovery and registers a dynamic
// informer for it. A CRD that is not installed is skipped quietly; any other
// failure is logged and drops this type only. It reports false when the type is skipped.
func (s *Source) crdInformer(stream *eventStream, factory dynamicinformer.DynamicSharedInformerFactory, kind crdKind, handlers []cache.ResourceEventHandler) (runningInformer, bool) {
	gvr, found, err := s.resolveGVR(stream.ctx, kind)
	if err != nil {
		if stream.ctx.Err() == nil {
			stream.log.Error("error discovering CRD, skipping type", "type", kind.dataType, "error", err.Error())
		}
		return runningInformer{}, false
	}
	if !found {
		stream.log.Info("CRD not installed, skipping type", "type", kind.dataType, "group", kind.group, "resource", kind.resource)
		return runningInformer{}, false
	}

	informer := factory.ForResource(gvr).Informer()
	if !stream.register(kind.dataType, informer, handlers...) {
		return runningInformer{}, false
	}
	return runningInformer{name: kind.dataType, informer: informer}, true
}

// register installs the handlers and the watch error logging on informer. It
// reports false, after logging, when the informer cannot be set up.
func (e *eventStream) register(name string, informer cache.SharedIndexInformer, handlers ...cache.ResourceEventHandler) bool {
	err := informer.SetWatchErrorHandlerWithContext(func(ctx context.Context, _ *cache.Reflector, err error) {
		if ctx.Err() == nil {
			e.log.Warn("error watching resources, retrying", "type", name, "error", err.Error())
		}
	})
	if err != nil {
		e.log.Error("error setting up informer", "type", name, "error", err.Error())
		return false
	}

	for _, handler := range handlers {
		if _, err := informer.AddEventHandler(handler); err != nil {
			e.log.Error("error registering informer handler", "type", name, "error", err.Error())
			return false
		}
	}
	return true
}

// waitForSync logs when the initial snapshot of the informer has been delivered, or
// an error when that takes longer than informerSyncTimeout. It returns when the
// informer synced or the stream is stopped, running onSynced in the former case.
func (e *eventStream) waitForSync(running runningInformer) {
	if !e.awaitSync(running) {
		return
	}
	e.log.Info("informer synced", "type", running.name)
	if running.onSynced != nil && e.ctx.Err() == nil {
		running.onSynced()
	}
}

// awaitSync reports whether the informer synced before the stream stopped.
func (e *eventStream) awaitSync(running runningInformer) bool {
	syncCtx, cancel := context.WithTimeout(e.ctx, informerSyncTimeout)
	defer cancel()

	if cache.WaitForCacheSync(syncCtx.Done(), running.informer.HasSynced) {
		return true
	}
	if e.ctx.Err() != nil {
		return false
	}

	e.log.Error("informer did not sync in time, still retrying", "type", running.name, "timeout", informerSyncTimeout.String())
	return cache.WaitForCacheSync(e.ctx.Done(), running.informer.HasSynced)
}

// emit sends a data item, giving up when the stream is stopping.
func (e *eventStream) emit(dataType string, operation source.DataOperation, values map[string]any) {
	data := source.Data{
		Type:      dataType,
		Operation: operation,
		Values:    values,
		Time:      timeSource(),
	}
	if err := send(e.ctx, e.results, data); err != nil {
		e.log.Debug("dropping event, stream stopping", "type", dataType)
	}
}

// handlers returns the handlers translating informer events of kind into data.
// Update events that do not change the resource version, or whose values equal
// the previous ones, are ignored. Objects matching kind.excluded are not items:
// their events are ignored, except the update that makes an object excluded, which
// deletes it, and the one that makes it no longer excluded, which upserts it.
func (e *eventStream) handlers(kind eventKind) cache.ResourceEventHandler {
	emitObject := func(obj any, operation source.DataOperation) {
		values, err := kind.values(obj, e.apiServer)
		if err != nil {
			e.log.Error("error building event values, skipping", "type", kind.name, "error", err.Error())
			return
		}
		e.emit(kind.name, operation, values)
	}

	unchanged := func(oldObj, newObj any) bool {
		oldValues, oldErr := kind.values(oldObj, e.apiServer)
		newValues, newErr := kind.values(newObj, e.apiServer)
		if oldErr != nil || newErr != nil {
			return false
		}
		if kind.comparable != nil {
			oldValues, newValues = kind.comparable(oldValues), kind.comparable(newValues)
		}
		return reflect.DeepEqual(oldValues, newValues)
	}

	excluded := func(obj any) bool {
		return kind.excluded != nil && kind.excluded(obj)
	}

	return cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			if excluded(obj) {
				return
			}
			emitObject(obj, source.DataOperationUpsert)
		},
		UpdateFunc: func(oldObj, newObj any) {
			oldExcluded, newExcluded := excluded(oldObj), excluded(newObj)
			switch {
			case oldExcluded && newExcluded:
				return
			case newExcluded:
				// The object stopped being an item: it was emitted so far, delete it.
				emitObject(oldObj, source.DataOperationDelete)
				return
			case oldExcluded:
				// The object became an item: it was never emitted.
				emitObject(newObj, source.DataOperationUpsert)
				return
			}
			if sameResourceVersion(oldObj, newObj) || unchanged(oldObj, newObj) {
				return
			}
			emitObject(newObj, source.DataOperationUpsert)
		},
		DeleteFunc: func(obj any) {
			obj = unwrapTombstone(obj)
			if excluded(obj) {
				return
			}
			emitObject(obj, source.DataOperationDelete)
		},
	}
}

// sameResourceVersion reports whether both objects carry the same, non empty, resource version.
func sameResourceVersion(oldObj, newObj any) bool {
	oldMeta, oldErr := meta.Accessor(oldObj)
	newMeta, newErr := meta.Accessor(newObj)
	if oldErr != nil || newErr != nil {
		return false
	}

	version := newMeta.GetResourceVersion()
	return version != "" && version == oldMeta.GetResourceVersion()
}

// unwrapTombstone returns the last known object held by a delete tombstone, or obj itself.
func unwrapTombstone(obj any) any {
	if tombstone, ok := obj.(cache.DeletedFinalStateUnknown); ok {
		return tombstone.Obj
	}
	return obj
}

// helmHandlers returns the handlers of the Helm release Secrets informer.
func (e *eventStream) helmHandlers(informer cache.SharedIndexInformer) cache.ResourceEventHandler {
	indexer := informer.GetIndexer()

	return cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			if secret, ok := obj.(*corev1.Secret); ok && isDeployedHelmSecret(secret) {
				e.helmUpsert(indexer, secret)
			}
		},
		UpdateFunc: func(oldObj, newObj any) {
			newSecret, newOK := newObj.(*corev1.Secret)
			if !newOK {
				return
			}
			if isDeployedHelmSecret(newSecret) {
				if !sameResourceVersion(oldObj, newObj) && !helmReleaseUnchanged(oldObj, newSecret, e.apiServer) {
					e.helmUpsert(indexer, newSecret)
				}
				return
			}
			// The Secret left the deployed set (a server-side filter reports this as a delete).
			if oldSecret, ok := oldObj.(*corev1.Secret); ok && isDeployedHelmSecret(oldSecret) {
				e.helmGone(indexer, newSecret)
			}
		},
		DeleteFunc: func(obj any) {
			if secret, ok := unwrapTombstone(obj).(*corev1.Secret); ok && secret.Labels[helmOwnerLabel] == helmOwnerValue {
				e.helmGone(indexer, secret)
			}
		},
	}
}

// helmReleaseUnchanged reports whether oldObj and newSecret are both deployed Helm
// Secrets that decode to the same release values. A Secret that cannot be decoded
// never counts as unchanged.
func helmReleaseUnchanged(oldObj any, newSecret *corev1.Secret, apiServer string) bool {
	oldSecret, ok := oldObj.(*corev1.Secret)
	if !ok || !isDeployedHelmSecret(oldSecret) {
		return false
	}

	oldValues, oldErr := helmSecretValues(oldSecret, apiServer)
	newValues, newErr := helmSecretValues(newSecret, apiServer)
	return oldErr == nil && newErr == nil && reflect.DeepEqual(oldValues, newValues)
}

// helmUpsert emits the release stored in secret, unless a deployed Secret with a
// higher revision exists for the same release: that one is the release to emit.
func (e *eventStream) helmUpsert(indexer cache.Indexer, secret *corev1.Secret) {
	if newest := newestDeployedHelmSecret(indexer, secret); newest != nil && helmSecretRevision(newest) > helmSecretRevision(secret) {
		e.log.Debug("skipping superseded helm release revision", "secret", secret.Name, "namespace", secret.Namespace)
		return
	}
	e.emitHelmSecret(secret, source.DataOperationUpsert)
}

// helmGone handles a Helm release Secret that disappeared from the deployed set. A
// release is deleted only when no other deployed Secret of the same release remains:
// on an upgrade the old revision becomes superseded while the release lives on.
func (e *eventStream) helmGone(indexer cache.Indexer, secret *corev1.Secret) {
	remaining := newestDeployedHelmSecret(indexer, secret)
	if remaining == nil {
		e.emitHelmSecret(secret, source.DataOperationDelete)
		return
	}

	// The release is still deployed; make sure its highest revision is the one that is current.
	if helmSecretRevision(secret) > helmSecretRevision(remaining) {
		e.emitHelmSecret(remaining, source.DataOperationUpsert)
	}
}

// emitHelmSecret decodes secret and emits the matching helmrelease data. A Secret
// that cannot be decoded is logged, without its content, and ignored.
func (e *eventStream) emitHelmSecret(secret *corev1.Secret, operation source.DataOperation) {
	values, err := helmSecretValues(secret, e.apiServer)
	if err != nil {
		e.log.Warn("error decoding helm release secret, skipping", "secret", secret.Name, "namespace", secret.Namespace, "error", err.Error())
		return
	}
	e.emit(helmReleaseType, operation, values)
}

// isDeployedHelmSecret reports whether secret stores the deployed revision of a Helm release.
func isDeployedHelmSecret(secret *corev1.Secret) bool {
	return secret.Labels[helmOwnerLabel] == helmOwnerValue && secret.Labels[helmStatusLabel] == helmStatusDeployed
}

// helmSecretRevision returns the release revision recorded in the Secret's version label.
func helmSecretRevision(secret *corev1.Secret) int {
	revision, err := strconv.Atoi(secret.Labels[helmVersionLabel])
	if err != nil {
		return 0
	}
	return revision
}

// newestDeployedHelmSecret returns, among the cached deployed Secrets of the same
// release as secret (same namespace and name label), the one with the highest
// revision, or nil when there is none. secret itself is never returned.
func newestDeployedHelmSecret(indexer cache.Indexer, secret *corev1.Secret) *corev1.Secret {
	cached, err := indexer.ByIndex(cache.NamespaceIndex, secret.Namespace)
	if err != nil {
		return nil
	}

	var newest *corev1.Secret
	for _, item := range cached {
		candidate, ok := item.(*corev1.Secret)
		if !ok || candidate.Name == secret.Name || !isDeployedHelmSecret(candidate) {
			continue
		}
		if candidate.Labels[helmNameLabel] != secret.Labels[helmNameLabel] {
			continue
		}
		if newest == nil || helmSecretRevision(candidate) > helmSecretRevision(newest) {
			newest = candidate
		}
	}
	return newest
}

// clusterEmitter turns the node informer events into the cluster item. The cluster
// values are recomputed from the informer cache and emitted only when they differ
// from the last emitted ones; the item is never deleted. The mutex serialises the
// initial emission with the handler callbacks and protects the last emitted values.
type clusterEmitter struct {
	stream  *eventStream
	indexer cache.Indexer

	mu     sync.Mutex
	synced bool
	last   map[string]any
}

// handlers returns the node informer handlers. The Add events of the initial list
// are ignored: emitInitial publishes the cluster once the cache is complete.
func (c *clusterEmitter) handlers() cache.ResourceEventHandler {
	return cache.ResourceEventHandlerDetailedFuncs{
		AddFunc: func(_ any, isInInitialList bool) {
			if !isInInitialList {
				c.refresh()
			}
		},
		UpdateFunc: func(oldObj, newObj any) {
			if !sameResourceVersion(oldObj, newObj) {
				c.refresh()
			}
		},
		DeleteFunc: func(any) {
			c.refresh()
		},
	}
}

// emitInitial emits the cluster computed from the synced node cache and enables
// the handlers to emit later changes.
func (c *clusterEmitter) emitInitial() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.synced = true
	c.last = c.values()
	c.stream.emit(clusterType, source.DataOperationUpsert, c.last)
}

// refresh recomputes the cluster values and emits them when they changed.
func (c *clusterEmitter) refresh() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.synced {
		return
	}

	values := c.values()
	if reflect.DeepEqual(c.last, values) {
		return
	}
	c.last = values
	c.stream.emit(clusterType, source.DataOperationUpsert, values)
}

// values builds the cluster values from the nodes in the informer cache.
func (c *clusterEmitter) values() map[string]any {
	cached := c.indexer.List()
	nodes := make([]corev1.Node, 0, len(cached))
	for _, item := range cached {
		node, ok := item.(*corev1.Node)
		if !ok {
			c.stream.log.Debug("ignoring unexpected object in node cache", "type", clusterType, "object", fmt.Sprintf("%T", item))
			continue
		}
		nodes = append(nodes, *node)
	}
	return clusterValues(c.stream.apiServer, c.stream.clusterName, nodes)
}
