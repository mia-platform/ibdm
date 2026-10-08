// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package k8s

import (
	"cmp"
	"context"
	"errors"
	"maps"
	"slices"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/tools/cache"

	"github.com/mia-platform/ibdm/internal/source"
)

const (
	// ingressRouteServiceRelationshipType is the data type key of the links between
	// an IngressRoute and the Services its routes point to.
	ingressRouteServiceRelationshipType = "ingressRouteServiceRelationship"

	// workloadHelmReleaseRelationshipType is the data type key of the links between
	// a workload and the Helm release that installed it.
	workloadHelmReleaseRelationshipType = "workloadHelmReleaseRelationship"

	// serviceWorkloadRelationshipType is the data type key of the links between a
	// Service and the workloads whose pod template its selector matches.
	serviceWorkloadRelationshipType = "serviceWorkloadRelationship"

	// helmReleaseNameAnnotation and helmReleaseNamespaceAnnotation are set by Helm
	// on every object it installs.
	helmReleaseNameAnnotation      = "meta.helm.sh/release-name"
	helmReleaseNamespaceAnnotation = "meta.helm.sh/release-namespace"

	// kindService is the kind of a route service entry that refers to a Kubernetes Service.
	kindService = "Service"

	// Catalog kinds of the workloads, as emitted in the "kind" value.
	kindDeployment  = "Deployment"
	kindStatefulSet = "StatefulSet"
	kindDaemonSet   = "DaemonSet"

	keyServiceNamespace  = "serviceNamespace"
	keyServiceName       = "serviceName"
	keyWorkloadKind      = "workloadKind"
	keyWorkloadName      = "workloadName"
	keyReleaseName       = "releaseName"
	keyReleaseNamespace  = "releaseNamespace"
	relationKeySeparator = "/"
)

// relation is one source/target pair derived from a Kubernetes object. key
// identifies the pair among the pairs of the same object; values are the data
// values emitted for it, for both upserts and deletes.
type relation struct {
	key    string
	values map[string]any
}

// pairsBuilder derives the relations of an informer object. It returns nil for an
// object of an unexpected type.
type pairsBuilder func(obj any, apiServer string) []relation

// relationshipBinding connects a relationship data type to the kind of object its
// pairs are derived from. A relationship type can have several bindings.
type relationshipBinding struct {
	relType    string
	sourceType string
	pairs      pairsBuilder
}

// relationshipBindings lists the relationship data types and the objects feeding them.
var relationshipBindings = []relationshipBinding{
	{
		relType:    ingressRouteServiceRelationshipType,
		sourceType: ingressRouteType,
		pairs:      unstructuredPairs(ingressRouteServicePairs),
	},
	{
		relType:    workloadHelmReleaseRelationshipType,
		sourceType: deploymentType,
		pairs: typedPairs(func(deployment *appsv1.Deployment, apiServer string) []relation {
			return workloadReleasePairs(kindDeployment, &deployment.ObjectMeta, apiServer)
		}),
	},
	{
		relType:    workloadHelmReleaseRelationshipType,
		sourceType: statefulSetType,
		pairs: typedPairs(func(statefulSet *appsv1.StatefulSet, apiServer string) []relation {
			return workloadReleasePairs(kindStatefulSet, &statefulSet.ObjectMeta, apiServer)
		}),
	},
	{
		relType:    workloadHelmReleaseRelationshipType,
		sourceType: daemonSetType,
		pairs: typedPairs(func(daemonSet *appsv1.DaemonSet, apiServer string) []relation {
			return workloadReleasePairs(kindDaemonSet, &daemonSet.ObjectMeta, apiServer)
		}),
	},
}

// isRelationshipType reports whether name is a relationship data type.
func isRelationshipType(name string) bool {
	return slices.ContainsFunc(relationshipBindings, func(binding relationshipBinding) bool { return binding.relType == name })
}

// typedPairs adapts a builder of *T to a pairsBuilder, ignoring objects of other types.
func typedPairs[T any](build func(*T, string) []relation) pairsBuilder {
	return func(obj any, apiServer string) []relation {
		typed, ok := obj.(*T)
		if !ok || typed == nil {
			return nil
		}
		return build(typed, apiServer)
	}
}

// unstructuredPairs adapts a builder of unstructured objects to a pairsBuilder.
func unstructuredPairs(build func(obj map[string]any, apiServer string) []relation) pairsBuilder {
	return func(obj any, apiServer string) []relation {
		unstructuredObj, ok := obj.(*unstructured.Unstructured)
		if !ok || unstructuredObj == nil {
			return nil
		}
		return build(unstructuredObj.Object, apiServer)
	}
}

// ingressRouteServicePairs returns one relation per distinct Service referenced by
// the routes of an IngressRoute. Entries of another kind (e.g. TraefikService) and
// entries without a name are ignored; an entry without namespace refers to the
// namespace of the route. The order of first appearance is kept.
func ingressRouteServicePairs(obj map[string]any, apiServer string) []relation {
	routeNamespace := nestedString(obj, "metadata", keyNamespace)
	routeName := nestedString(obj, "metadata", keyName)

	var pairs []relation
	seen := make(map[string]struct{})
	for _, route := range nestedMaps(obj, "spec", "routes") {
		for _, service := range nestedMaps(route, "services") {
			if kind := stringField(service, keyKind); kind != "" && kind != kindService {
				continue
			}
			name := stringField(service, keyName)
			if name == "" {
				continue
			}
			namespace := stringField(service, keyNamespace)
			if namespace == "" {
				namespace = routeNamespace
			}

			key := namespace + relationKeySeparator + name
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			pairs = append(pairs, relation{key: key, values: map[string]any{
				keyAPIServer:        apiServer,
				keyNamespace:        routeNamespace,
				keyName:             routeName,
				keyServiceNamespace: namespace,
				keyServiceName:      name,
			}})
		}
	}
	return pairs
}

// workloadReleasePairs returns the relation between a workload of the given kind and
// the Helm release named by its annotations, or nil when either annotation is missing or empty.
func workloadReleasePairs(kind string, meta *metav1.ObjectMeta, apiServer string) []relation {
	releaseName := meta.Annotations[helmReleaseNameAnnotation]
	releaseNamespace := meta.Annotations[helmReleaseNamespaceAnnotation]
	if releaseName == "" || releaseNamespace == "" {
		return nil
	}

	return []relation{{
		key: releaseNamespace + relationKeySeparator + releaseName,
		values: map[string]any{
			keyAPIServer:        apiServer,
			keyKind:             kind,
			keyNamespace:        meta.Namespace,
			keyName:             meta.Name,
			keyReleaseName:      releaseName,
			keyReleaseNamespace: releaseNamespace,
		},
	}}
}

// syncIngressRouteServiceRelationships emits one item per distinct pair of
// IngressRoute and Service. A cluster without the Traefik CRD is skipped.
func (s *Source) syncIngressRouteServiceRelationships(ctx context.Context, results chan<- source.Data) error {
	return s.listCRD(ctx, ingressRouteKind, ingressRouteServiceRelationshipType, func(obj *unstructured.Unstructured) error {
		return s.sendRelations(ctx, results, ingressRouteServiceRelationshipType, ingressRouteServicePairs(obj.Object, s.apiServer))
	})
}

// syncWorkloadHelmReleaseRelationships emits one item per Deployment, StatefulSet
// and DaemonSet carrying the Helm release annotations. A failure listing one
// workload kind does not prevent the others from being listed.
func (s *Source) syncWorkloadHelmReleaseRelationships(ctx context.Context, results chan<- source.Data) error {
	emit := func(kind string, meta *metav1.ObjectMeta) error {
		return s.sendRelations(ctx, results, workloadHelmReleaseRelationshipType, workloadReleasePairs(kind, meta, s.apiServer))
	}

	errs := []error{
		listPages(ctx, "deployments",
			func(ctx context.Context, options metav1.ListOptions) ([]appsv1.Deployment, string, error) {
				list, err := s.clientset.AppsV1().Deployments(metav1.NamespaceAll).List(ctx, options)
				if err != nil {
					return nil, "", err
				}
				return list.Items, list.Continue, nil
			},
			func(deployment *appsv1.Deployment) error { return emit(kindDeployment, &deployment.ObjectMeta) },
		),
		listPages(ctx, "statefulsets",
			func(ctx context.Context, options metav1.ListOptions) ([]appsv1.StatefulSet, string, error) {
				list, err := s.clientset.AppsV1().StatefulSets(metav1.NamespaceAll).List(ctx, options)
				if err != nil {
					return nil, "", err
				}
				return list.Items, list.Continue, nil
			},
			func(statefulSet *appsv1.StatefulSet) error { return emit(kindStatefulSet, &statefulSet.ObjectMeta) },
		),
		listPages(ctx, "daemonsets",
			func(ctx context.Context, options metav1.ListOptions) ([]appsv1.DaemonSet, string, error) {
				list, err := s.clientset.AppsV1().DaemonSets(metav1.NamespaceAll).List(ctx, options)
				if err != nil {
					return nil, "", err
				}
				return list.Items, list.Continue, nil
			},
			func(daemonSet *appsv1.DaemonSet) error { return emit(kindDaemonSet, &daemonSet.ObjectMeta) },
		),
	}
	return errors.Join(errs...)
}

// sendRelations emits an upsert item of dataType for every relation.
func (s *Source) sendRelations(ctx context.Context, results chan<- source.Data, dataType string, relations []relation) error {
	for _, rel := range relations {
		if err := send(ctx, results, s.workloadData(dataType, rel.values)); err != nil {
			return err
		}
	}
	return nil
}

// relationHandlers returns the informer handlers emitting the relationship items of
// binding. Add upserts every relation of the object; Update upserts the relations
// that are new and deletes the ones that disappeared, emitting nothing when the set
// is unchanged; Delete deletes every relation of the last known object.
func (e *eventStream) relationHandlers(binding relationshipBinding) cache.ResourceEventHandler {
	emitAll := func(relations []relation, operation source.DataOperation) {
		for _, rel := range relations {
			e.emit(binding.relType, operation, rel.values)
		}
	}

	return cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			emitAll(binding.pairs(obj, e.apiServer), source.DataOperationUpsert)
		},
		UpdateFunc: func(oldObj, newObj any) {
			if sameResourceVersion(oldObj, newObj) {
				return
			}

			oldRelations := binding.pairs(oldObj, e.apiServer)
			newRelations := binding.pairs(newObj, e.apiServer)
			emitAll(relationsMissingFrom(newRelations, oldRelations), source.DataOperationUpsert)
			emitAll(relationsMissingFrom(oldRelations, newRelations), source.DataOperationDelete)
		},
		DeleteFunc: func(obj any) {
			emitAll(binding.pairs(unwrapTombstone(obj), e.apiServer), source.DataOperationDelete)
		},
	}
}

// relationsMissingFrom returns the relations of candidates whose key is not in others.
func relationsMissingFrom(candidates, others []relation) []relation {
	var missing []relation
	for _, candidate := range candidates {
		if !slices.ContainsFunc(others, func(other relation) bool { return other.key == candidate.key }) {
			missing = append(missing, candidate)
		}
	}
	return missing
}

// sourceHandlers returns the handlers to attach to the informer of the object kind
// kind: the item handlers when its data type is requested, plus those of every
// requested relationship type fed by it. It is empty when nothing needs the informer.
func (e *eventStream) sourceHandlers(kind eventKind, requested map[string]struct{}) []cache.ResourceEventHandler {
	var handlers []cache.ResourceEventHandler
	if _, ok := requested[kind.name]; ok {
		handlers = append(handlers, e.handlers(kind))
	}
	for _, binding := range relationshipBindings {
		if _, ok := requested[binding.relType]; ok && binding.sourceType == kind.name {
			handlers = append(handlers, e.relationHandlers(binding))
		}
	}
	for _, reconciler := range e.reconcilers {
		if handler := reconciler.handlerFor(kind.name); handler != nil {
			handlers = append(handlers, handler)
		}
	}
	return handlers
}

// serviceRef is the part of a Service that selects pods.
type serviceRef struct {
	name     string
	selector map[string]string
}

// workloadRef is the part of a workload that the selector of a Service is matched against.
type workloadRef struct {
	kind           string
	name           string
	templateLabels map[string]string
}

// selectorMatches reports whether labels contain every key/value pair of selector.
// An empty selector matches nothing: a Service without selector does not front pods
// through the cluster's selection mechanism.
func selectorMatches(selector, labels map[string]string) bool {
	if len(selector) == 0 {
		return false
	}
	for key, value := range selector {
		if labelValue, ok := labels[key]; !ok || labelValue != value {
			return false
		}
	}
	return true
}

// serviceWorkloadPairs returns one relation for every Service of namespace that
// selects a workload of the same namespace, ordered by service name, workload kind
// and workload name. Neither input is modified.
func serviceWorkloadPairs(apiServer, namespace string, services []serviceRef, workloads []workloadRef) []relation {
	sortedServices := slices.SortedFunc(slices.Values(services), func(a, b serviceRef) int {
		return strings.Compare(a.name, b.name)
	})
	sortedWorkloads := slices.SortedFunc(slices.Values(workloads), func(a, b workloadRef) int {
		return cmp.Or(strings.Compare(a.kind, b.kind), strings.Compare(a.name, b.name))
	})

	var pairs []relation
	for _, service := range sortedServices {
		if len(service.selector) == 0 {
			continue
		}
		for _, workload := range sortedWorkloads {
			if !selectorMatches(service.selector, workload.templateLabels) {
				continue
			}
			pairs = append(pairs, relation{
				key: service.name + relationKeySeparator + workload.kind + relationKeySeparator + workload.name,
				values: map[string]any{
					keyAPIServer:    apiServer,
					keyNamespace:    namespace,
					keyServiceName:  service.name,
					keyWorkloadKind: workload.kind,
					keyWorkloadName: workload.name,
				},
			})
		}
	}
	return pairs
}

// serviceRefOf extracts the selector of a Service informer object.
func serviceRefOf(obj any) (serviceRef, bool) {
	service, ok := obj.(*corev1.Service)
	if !ok || service == nil {
		return serviceRef{}, false
	}
	return serviceRef{name: service.Name, selector: service.Spec.Selector}, true
}

// workloadRefOf extracts the pod template labels of a Deployment, StatefulSet or
// DaemonSet informer object.
func workloadRefOf(obj any) (workloadRef, bool) {
	switch workload := obj.(type) {
	case *appsv1.Deployment:
		if workload != nil {
			return workloadRef{kind: kindDeployment, name: workload.Name, templateLabels: workload.Spec.Template.Labels}, true
		}
	case *appsv1.StatefulSet:
		if workload != nil {
			return workloadRef{kind: kindStatefulSet, name: workload.Name, templateLabels: workload.Spec.Template.Labels}, true
		}
	case *appsv1.DaemonSet:
		if workload != nil {
			return workloadRef{kind: kindDaemonSet, name: workload.Name, templateLabels: workload.Spec.Template.Labels}, true
		}
	}
	return workloadRef{}, false
}

// serviceWorkloadSources lists the informer kinds feeding serviceWorkloadRelationship
// and, for each, how to tell that an update may change the pairs.
var serviceWorkloadSources = []reconcileSource{
	{kind: serviceType, changed: func(oldObj, newObj any) bool {
		oldService, oldOK := serviceRefOf(oldObj)
		newService, newOK := serviceRefOf(newObj)
		return !oldOK || !newOK || !maps.Equal(oldService.selector, newService.selector)
	}},
	{kind: deploymentType, changed: workloadLabelsChanged},
	{kind: statefulSetType, changed: workloadLabelsChanged},
	{kind: daemonSetType, changed: workloadLabelsChanged},
}

// workloadLabelsChanged reports whether the pod template labels differ between two
// workload objects; an unexpected object counts as changed.
func workloadLabelsChanged(oldObj, newObj any) bool {
	oldWorkload, oldOK := workloadRefOf(oldObj)
	newWorkload, newOK := workloadRefOf(newObj)
	return !oldOK || !newOK || !maps.Equal(oldWorkload.templateLabels, newWorkload.templateLabels)
}

// newServiceWorkloadReconciler returns the reconciler emitting the
// serviceWorkloadRelationship items in watch mode.
func newServiceWorkloadReconciler(stream *eventStream) *pairReconciler {
	return newPairReconciler(stream, serviceWorkloadRelationshipType, serviceWorkloadSources,
		func(indexers map[string]cache.Indexer, namespace string) []relation {
			var services []serviceRef
			for _, obj := range indexedIn(indexers[serviceType], namespace) {
				if service, ok := serviceRefOf(obj); ok {
					services = append(services, service)
				}
			}

			var workloads []workloadRef
			for _, dataType := range []string{deploymentType, statefulSetType, daemonSetType} {
				for _, obj := range indexedIn(indexers[dataType], namespace) {
					if workload, ok := workloadRefOf(obj); ok {
						workloads = append(workloads, workload)
					}
				}
			}
			return serviceWorkloadPairs(stream.apiServer, namespace, services, workloads)
		},
	)
}

// syncServiceWorkloadRelationships emits one item per Service and workload pair
// where the selector of the Service matches the pod template labels of a Deployment,
// StatefulSet or DaemonSet of its namespace. A failure listing one kind does not
// prevent the others from being listed: the pairs computable from what was listed
// are emitted and the failures are returned.
func (s *Source) syncServiceWorkloadRelationships(ctx context.Context, results chan<- source.Data) error {
	services := make(map[string][]serviceRef)
	workloads := make(map[string][]workloadRef)
	addWorkload := func(kind string, meta *metav1.ObjectMeta, template *corev1.PodTemplateSpec) {
		workloads[meta.Namespace] = append(workloads[meta.Namespace], workloadRef{kind: kind, name: meta.Name, templateLabels: template.Labels})
	}

	errs := []error{
		listPages(ctx, "services",
			func(ctx context.Context, options metav1.ListOptions) ([]corev1.Service, string, error) {
				list, err := s.clientset.CoreV1().Services(metav1.NamespaceAll).List(ctx, options)
				if err != nil {
					return nil, "", err
				}
				return list.Items, list.Continue, nil
			},
			func(service *corev1.Service) error {
				services[service.Namespace] = append(services[service.Namespace], serviceRef{name: service.Name, selector: service.Spec.Selector})
				return nil
			},
		),
		listPages(ctx, "deployments",
			func(ctx context.Context, options metav1.ListOptions) ([]appsv1.Deployment, string, error) {
				list, err := s.clientset.AppsV1().Deployments(metav1.NamespaceAll).List(ctx, options)
				if err != nil {
					return nil, "", err
				}
				return list.Items, list.Continue, nil
			},
			func(deployment *appsv1.Deployment) error {
				addWorkload(kindDeployment, &deployment.ObjectMeta, &deployment.Spec.Template)
				return nil
			},
		),
		listPages(ctx, "statefulsets",
			func(ctx context.Context, options metav1.ListOptions) ([]appsv1.StatefulSet, string, error) {
				list, err := s.clientset.AppsV1().StatefulSets(metav1.NamespaceAll).List(ctx, options)
				if err != nil {
					return nil, "", err
				}
				return list.Items, list.Continue, nil
			},
			func(statefulSet *appsv1.StatefulSet) error {
				addWorkload(kindStatefulSet, &statefulSet.ObjectMeta, &statefulSet.Spec.Template)
				return nil
			},
		),
		listPages(ctx, "daemonsets",
			func(ctx context.Context, options metav1.ListOptions) ([]appsv1.DaemonSet, string, error) {
				list, err := s.clientset.AppsV1().DaemonSets(metav1.NamespaceAll).List(ctx, options)
				if err != nil {
					return nil, "", err
				}
				return list.Items, list.Continue, nil
			},
			func(daemonSet *appsv1.DaemonSet) error {
				addWorkload(kindDaemonSet, &daemonSet.ObjectMeta, &daemonSet.Spec.Template)
				return nil
			},
		),
	}

	// Only namespaces with services can produce pairs.
	for _, namespace := range slices.Sorted(maps.Keys(services)) {
		pairs := serviceWorkloadPairs(s.apiServer, namespace, services[namespace], workloads[namespace])
		if err := s.sendRelations(ctx, results, serviceWorkloadRelationshipType, pairs); err != nil {
			return errors.Join(append(errs, err)...)
		}
	}
	return errors.Join(errs...)
}
