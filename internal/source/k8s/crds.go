// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package k8s

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/mia-platform/ibdm/internal/source"
)

const (
	traefikGroup         = "traefik.io"
	ingressRoutesRes     = "ingressroutes"
	certManagerGroup     = "cert-manager.io"
	certificatesRes      = "certificates"
	certManagerVersion   = "v1"
	conditionTypeReady   = "Ready"
	conditionStatusUnset = "Unknown"
)

// Value keys shared by the CRD-backed data types.
const (
	keyNamespace = "namespace"
	keyLabels    = "labels"
	keyKind      = "kind"
)

// crdKind describes a CRD-backed data type read through the dynamic client.
type crdKind struct {
	dataType string
	group    string
	resource string
	// preferredVersion, when served by the cluster, wins over the version preferred by discovery.
	preferredVersion string
	// values builds the emitted values from one unstructured object.
	values func(apiServer string, obj map[string]any) map[string]any
}

var (
	ingressRouteKind = crdKind{
		dataType: ingressRouteType,
		group:    traefikGroup,
		resource: ingressRoutesRes,
		values:   ingressRouteValues,
	}
	certificateKind = crdKind{
		dataType:         certificateType,
		group:            certManagerGroup,
		resource:         certificatesRes,
		preferredVersion: certManagerVersion,
		values:           certificateValues,
	}
)

// crdKinds lists the CRD-backed data types.
var crdKinds = []crdKind{ingressRouteKind, certificateKind}

// objectValues builds the values of one object of kind. It is shared by the
// sync loop and the informer handlers.
func (k crdKind) objectValues(obj *unstructured.Unstructured, apiServer string) map[string]any {
	return k.values(apiServer, obj.Object)
}

// eventKind adapts kind to the generic event handlers, which receive the
// *unstructured.Unstructured objects of a dynamic informer.
func (k crdKind) eventKind() eventKind {
	return eventKind{
		name: k.dataType,
		values: func(obj any, apiServer string) (map[string]any, error) {
			unstructuredObj, ok := obj.(*unstructured.Unstructured)
			if !ok || unstructuredObj == nil {
				return nil, fmt.Errorf("%w: %T", errUnexpectedObject, obj)
			}
			return k.objectValues(unstructuredObj, apiServer), nil
		},
	}
}

// syncIngressRoutes emits one ingressroute item per Traefik IngressRoute.
func (s *Source) syncIngressRoutes(ctx context.Context, results chan<- source.Data) error {
	return s.syncCRD(ctx, results, ingressRouteKind)
}

// syncCertificates emits one certificate item per cert-manager Certificate.
func (s *Source) syncCertificates(ctx context.Context, results chan<- source.Data) error {
	return s.syncCRD(ctx, results, certificateKind)
}

// syncCRD resolves the GVR of kind through discovery and lists every object of
// the cluster. A kind whose CRD is not installed is skipped without error.
func (s *Source) syncCRD(ctx context.Context, results chan<- source.Data, kind crdKind) error {
	return s.listCRD(ctx, kind, kind.dataType, func(obj *unstructured.Unstructured) error {
		return send(ctx, results, s.workloadData(kind.dataType, kind.values(s.apiServer, obj.Object)))
	})
}

// listCRD resolves the GVR of kind through discovery and calls handle for every
// object of the cluster. A kind whose CRD is not installed is skipped without
// error; dataType is the type being synced, used for logging.
func (s *Source) listCRD(ctx context.Context, kind crdKind, dataType string, handle func(*unstructured.Unstructured) error) error {
	log := logger.FromContext(ctx).WithName(loggerName)

	gvr, found, err := s.resolveGVR(ctx, kind)
	if err != nil {
		return err
	}
	if !found {
		log.Info("CRD not installed, skipping type", "type", dataType, "group", kind.group, "resource", kind.resource)
		return nil
	}

	return listPages(ctx, kind.resource,
		func(ctx context.Context, options metav1.ListOptions) ([]unstructured.Unstructured, string, error) {
			list, err := s.dynamic.Resource(gvr).Namespace(metav1.NamespaceAll).List(ctx, options)
			if err != nil {
				return nil, "", err
			}
			return list.Items, list.GetContinue(), nil
		},
		handle,
	)
}

// resolveGVR looks up through discovery the version of kind.group serving
// kind.resource. Candidate versions are tried in order: kind.preferredVersion,
// the version preferred by the server, then the remaining served versions.
// found is false, with a nil error, when the group or the resource is not
// served, i.e. the CRD is not installed.
func (s *Source) resolveGVR(ctx context.Context, kind crdKind) (gvr schema.GroupVersionResource, found bool, err error) {
	if err := ctx.Err(); err != nil {
		return gvr, false, err
	}

	discovery := s.clientset.Discovery()
	groups, err := discovery.ServerGroups()
	if err != nil {
		return gvr, false, fmt.Errorf("%w: discovering group %s: %w", ErrRetrievingAssets, kind.group, err)
	}

	for _, group := range groups.Groups {
		if group.Name != kind.group {
			continue
		}

		for _, version := range candidateVersions(kind.preferredVersion, group) {
			resources, err := discovery.ServerResourcesForGroupVersion(schema.GroupVersion{Group: kind.group, Version: version}.String())
			if apierrors.IsNotFound(err) {
				continue
			}
			if err != nil {
				return gvr, false, fmt.Errorf("%w: discovering %s/%s: %w", ErrRetrievingAssets, kind.group, version, err)
			}

			for _, resource := range resources.APIResources {
				if resource.Name == kind.resource {
					return schema.GroupVersionResource{Group: kind.group, Version: version, Resource: kind.resource}, true, nil
				}
			}
		}
	}

	return gvr, false, nil
}

// candidateVersions returns the versions of group to probe, most wanted first and without duplicates.
func candidateVersions(wanted string, group metav1.APIGroup) []string {
	ordered := make([]string, 0, 2+len(group.Versions))
	ordered = append(ordered, wanted, group.PreferredVersion.Version)
	for _, version := range group.Versions {
		ordered = append(ordered, version.Version)
	}

	seen := make(map[string]struct{}, len(ordered))
	versions := make([]string, 0, len(ordered))
	for _, version := range ordered {
		if _, ok := seen[version]; version == "" || ok {
			continue
		}
		seen[version] = struct{}{}
		versions = append(versions, version)
	}
	return versions
}

// ingressRouteValues builds the values of a Traefik IngressRoute.
func ingressRouteValues(apiServer string, obj map[string]any) map[string]any {
	routeSpecs := nestedMaps(obj, "spec", "routes")
	routes := make([]map[string]any, 0, len(routeSpecs))
	for _, route := range routeSpecs {
		services := mapList(nestedMaps(route, "services"), func(service map[string]any) map[string]any {
			return map[string]any{
				keyName:      stringField(service, keyName),
				keyKind:      stringField(service, keyKind),
				keyNamespace: stringField(service, keyNamespace),
				"port":       portField(service),
			}
		})
		middlewares := mapList(nestedMaps(route, "middlewares"), func(middleware map[string]any) map[string]any {
			return map[string]any{
				keyName:      stringField(middleware, keyName),
				keyNamespace: stringField(middleware, keyNamespace),
			}
		})

		routes = append(routes, map[string]any{
			"match":       stringField(route, "match"),
			"services":    services,
			"middlewares": middlewares,
		})
	}

	tls := map[string]any{}
	if tlsSpec, ok := nestedMap(obj, "spec", "tls"); ok {
		options, _ := nestedMap(tlsSpec, "options")
		tls = map[string]any{
			"secretName":   stringField(tlsSpec, "secretName"),
			"certResolver": stringField(tlsSpec, "certResolver"),
			"options":      stringField(options, keyName),
		}
	}

	values := crdBaseValues(apiServer, obj)
	values["entryPoints"] = nestedStrings(obj, "spec", "entryPoints")
	values["routes"] = routes
	values["tls"] = tls
	return values
}

// mapList converts every item of items, returning a non-nil slice.
func mapList(items []map[string]any, convert func(map[string]any) map[string]any) []map[string]any {
	converted := make([]map[string]any, 0, len(items))
	for _, item := range items {
		converted = append(converted, convert(item))
	}
	return converted
}

// certificateValues builds the values of a cert-manager Certificate.
func certificateValues(apiServer string, obj map[string]any) map[string]any {
	issuer, _ := nestedMap(obj, "spec", "issuerRef")

	status, reason := conditionStatusUnset, ""
	for _, condition := range nestedMaps(obj, "status", "conditions") {
		if stringField(condition, "type") != conditionTypeReady {
			continue
		}
		if value := stringField(condition, "status"); value != "" {
			status = value
		}
		reason = stringField(condition, "reason")
		break
	}

	values := crdBaseValues(apiServer, obj)
	values["dnsNames"] = nestedStrings(obj, "spec", "dnsNames")
	values["issuer"] = map[string]any{
		keyName: stringField(issuer, keyName),
		keyKind: stringField(issuer, keyKind),
		"group": stringField(issuer, "group"),
	}
	values["status"] = status
	values["statusReason"] = reason
	values["renewalTime"] = nestedString(obj, "status", "renewalTime")
	values["notAfter"] = nestedString(obj, "status", "notAfter")
	return values
}

// crdBaseValues builds the values shared by every CRD-backed type.
func crdBaseValues(apiServer string, obj map[string]any) map[string]any {
	labels := map[string]string{}
	for key, value := range nestedStringMap(obj, "metadata", keyLabels) {
		labels[key] = value
	}

	return map[string]any{
		keyAPIServer: apiServer,
		keyName:      nestedString(obj, "metadata", keyName),
		keyNamespace: nestedString(obj, "metadata", keyNamespace),
		keyLabels:    labels,
	}
}

// nestedField reads a value without copying it, so that unexpected value
// types can never make deep-copy helpers panic.
func nestedField(obj map[string]any, fields ...string) (any, bool) {
	value, found, err := unstructured.NestedFieldNoCopy(obj, fields...)
	if err != nil || !found {
		return nil, false
	}
	return value, true
}

func nestedString(obj map[string]any, fields ...string) string {
	value, _ := nestedField(obj, fields...)
	str, _ := value.(string)
	return str
}

func nestedMap(obj map[string]any, fields ...string) (map[string]any, bool) {
	value, ok := nestedField(obj, fields...)
	if !ok {
		return nil, false
	}
	m, ok := value.(map[string]any)
	return m, ok
}

// nestedMaps returns the object items of a list field, ignoring other items. It is never nil.
func nestedMaps(obj map[string]any, fields ...string) []map[string]any {
	items := make([]map[string]any, 0)
	value, _ := nestedField(obj, fields...)
	list, _ := value.([]any)
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			items = append(items, m)
		}
	}
	return items
}

// nestedStrings returns the string items of a list field, ignoring other items. It is never nil.
func nestedStrings(obj map[string]any, fields ...string) []string {
	items := make([]string, 0)
	value, _ := nestedField(obj, fields...)
	list, _ := value.([]any)
	for _, item := range list {
		if str, ok := item.(string); ok {
			items = append(items, str)
		}
	}
	return items
}

// nestedStringMap returns the string entries of a map field. It is never nil.
func nestedStringMap(obj map[string]any, fields ...string) map[string]string {
	entries := map[string]string{}
	m, _ := nestedMap(obj, fields...)
	for key, value := range m {
		if str, ok := value.(string); ok {
			entries[key] = str
		}
	}
	return entries
}

// stringField reads a string key of m, returning "" when absent or not a string.
func stringField(m map[string]any, key string) string {
	str, _ := m[key].(string)
	return str
}

// portField returns the service port as given (number or name), or "" when absent.
func portField(service map[string]any) any {
	switch port := service["port"].(type) {
	case string, int, int32, int64, float64:
		return port
	default:
		return ""
	}
}
