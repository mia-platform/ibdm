// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package k8s

import (
	"maps"
	"slices"
	"sync"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/tools/cache"

	"github.com/mia-platform/ibdm/internal/source"
)

// reconcileSource is an informer kind a pairReconciler depends on.
type reconcileSource struct {
	kind string
	// changed reports whether an update of an object of this kind may change the
	// pairs; updates for which it returns false are ignored.
	changed func(oldObj, newObj any) bool
}

// pairsComputer derives the relations of a namespace from the informer caches.
// The result must be ordered deterministically and every key unique.
type pairsComputer func(indexers map[string]cache.Indexer, namespace string) []relation

// pairReconciler emits a relationship data type that depends on several informers.
// It reads the informer caches, keeps the set of pairs last emitted per namespace
// and, after each relevant event, emits an upsert for every pair that is new and a
// delete for every pair that disappeared. Nothing is emitted until all the
// informers it depends on have synced; the full set is then emitted once. The
// namespace is the unit of recomputation: objects of different namespaces never
// pair with each other.
type pairReconciler struct {
	stream  *eventStream
	relType string
	sources map[string]reconcileSource
	compute pairsComputer

	mu       sync.Mutex
	indexers map[string]cache.Indexer
	synced   map[string]bool
	ready    bool
	// last holds, per namespace, the values of the pairs last emitted by pair key.
	last map[string]map[string]map[string]any
}

// newPairReconciler returns a reconciler of relType depending on sources.
func newPairReconciler(stream *eventStream, relType string, sources []reconcileSource, compute pairsComputer) *pairReconciler {
	byKind := make(map[string]reconcileSource, len(sources))
	for _, src := range sources {
		byKind[src.kind] = src
	}
	return &pairReconciler{
		stream:   stream,
		relType:  relType,
		sources:  byKind,
		compute:  compute,
		indexers: make(map[string]cache.Indexer, len(sources)),
		synced:   make(map[string]bool, len(sources)),
		last:     make(map[string]map[string]map[string]any),
	}
}

// newReconcilers returns the reconcilers of the requested relationship types that
// depend on several informers.
func (e *eventStream) newReconcilers(requested map[string]struct{}) []*pairReconciler {
	var reconcilers []*pairReconciler
	if _, ok := requested[serviceWorkloadRelationshipType]; ok {
		reconcilers = append(reconcilers, newServiceWorkloadReconciler(e))
	}
	return reconcilers
}

// bindReconcilers connects every reconciler depending on kind to the informer cache
// and returns the function to run when that informer has synced, or nil when no
// reconciler depends on kind. It must be called before the informers start.
func (e *eventStream) bindReconcilers(kind string, informer cache.SharedIndexInformer) func() {
	var bound []*pairReconciler
	for _, reconciler := range e.reconcilers {
		if _, ok := reconciler.sources[kind]; ok {
			reconciler.indexers[kind] = informer.GetIndexer()
			bound = append(bound, reconciler)
		}
	}
	if len(bound) == 0 {
		return nil
	}

	return func() {
		for _, reconciler := range bound {
			reconciler.informerSynced(kind)
		}
	}
}

// handlerFor returns the handler of the informer of kind, or nil when the
// reconciler does not depend on kind. The Add events of the initial list are
// ignored: the initial emission covers the synced caches.
func (r *pairReconciler) handlerFor(kind string) cache.ResourceEventHandler {
	src, ok := r.sources[kind]
	if !ok {
		return nil
	}

	return cache.ResourceEventHandlerDetailedFuncs{
		AddFunc: func(obj any, isInInitialList bool) {
			if !isInInitialList {
				r.refresh(obj)
			}
		},
		UpdateFunc: func(oldObj, newObj any) {
			if sameResourceVersion(oldObj, newObj) || (src.changed != nil && !src.changed(oldObj, newObj)) {
				return
			}
			r.refresh(newObj)
		},
		DeleteFunc: func(obj any) {
			r.refresh(unwrapTombstone(obj))
		},
	}
}

// informerSynced records that the informer of kind synced. When it was the last
// one, the full pair set is emitted and the handlers are enabled.
func (r *pairReconciler) informerSynced(kind string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.synced[kind] = true
	if r.ready || len(r.synced) < len(r.sources) {
		return
	}

	r.ready = true
	for _, namespace := range r.namespaces() {
		r.reconcile(namespace)
	}
}

// refresh recomputes the pairs of the namespace of obj, once the initial emission is done.
func (r *pairReconciler) refresh(obj any) {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		r.stream.log.Debug("ignoring unexpected object", "type", r.relType, "error", err.Error())
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.ready {
		r.reconcile(accessor.GetNamespace())
	}
}

// namespaces returns the sorted namespaces holding objects in any informer cache.
func (r *pairReconciler) namespaces() []string {
	unique := make(map[string]struct{})
	for _, indexer := range r.indexers {
		for _, namespace := range indexer.ListIndexFuncValues(cache.NamespaceIndex) {
			unique[namespace] = struct{}{}
		}
	}
	return slices.Sorted(maps.Keys(unique))
}

// reconcile emits the difference between the pairs computed for namespace and the
// ones last emitted: upserts of the new pairs first, then deletes of the vanished
// ones, each in key order. The caller holds r.mu.
func (r *pairReconciler) reconcile(namespace string) {
	previous := r.last[namespace]
	current := make(map[string]map[string]any)

	for _, rel := range r.compute(r.indexers, namespace) {
		current[rel.key] = rel.values
		if _, ok := previous[rel.key]; !ok {
			r.stream.emit(r.relType, source.DataOperationUpsert, rel.values)
		}
	}

	for _, key := range slices.Sorted(maps.Keys(previous)) {
		if _, ok := current[key]; !ok {
			r.stream.emit(r.relType, source.DataOperationDelete, previous[key])
		}
	}

	if len(current) == 0 {
		delete(r.last, namespace)
		return
	}
	r.last[namespace] = current
}

// indexedIn returns the objects of the indexer in namespace; a nil indexer or a
// failing lookup yields none.
func indexedIn(indexer cache.Indexer, namespace string) []any {
	if indexer == nil {
		return nil
	}
	objects, err := indexer.ByIndex(cache.NamespaceIndex, namespace)
	if err != nil {
		return nil
	}
	return objects
}
