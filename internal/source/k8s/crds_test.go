// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package k8s

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	clienttesting "k8s.io/client-go/testing"

	"github.com/mia-platform/ibdm/internal/source"
)

var (
	traefikV1Alpha1 = schema.GroupVersionResource{Group: traefikGroup, Version: "v1alpha1", Resource: ingressRoutesRes}
	traefikV3       = schema.GroupVersionResource{Group: traefikGroup, Version: "v3", Resource: ingressRoutesRes}
	certManagerV1   = schema.GroupVersionResource{Group: certManagerGroup, Version: "v1", Resource: certificatesRes}
)

// serverResources builds a discovery entry serving resources in group/version.
func serverResources(groupVersion string, resources ...string) *metav1.APIResourceList {
	list := &metav1.APIResourceList{GroupVersion: groupVersion}
	for _, name := range resources {
		list.APIResources = append(list.APIResources, metav1.APIResource{Name: name, Namespaced: true})
	}
	return list
}

// newCRDSource creates a Source with fake discovery, clientset and dynamic client.
func newCRDSource(t *testing.T, served []*metav1.APIResourceList, objects ...runtime.Object) *Source {
	t.Helper()

	clientset := fake.NewClientset()
	discovery, ok := clientset.Discovery().(*fakediscovery.FakeDiscovery)
	require.True(t, ok)
	discovery.Resources = served

	listKinds := map[schema.GroupVersionResource]string{
		traefikV1Alpha1: "IngressRouteList",
		traefikV3:       "IngressRouteList",
		certManagerV1:   "CertificateList",
	}

	return &Source{
		apiServer:   testAPIServer,
		clusterName: testClusterName,
		clientset:   clientset,
		dynamic:     dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), listKinds, objects...),
	}
}

func allServed() []*metav1.APIResourceList {
	return []*metav1.APIResourceList{
		serverResources("traefik.io/v1alpha1", ingressRoutesRes, "middlewares"),
		serverResources("cert-manager.io/v1", certificatesRes, "issuers"),
	}
}

func newIngressRouteObject(namespace, name string, labels map[string]any, spec map[string]any) *unstructured.Unstructured {
	metadata := map[string]any{"namespace": namespace, "name": name}
	if labels != nil {
		metadata["labels"] = labels
	}
	obj := map[string]any{"apiVersion": "traefik.io/v1alpha1", "kind": "IngressRoute", "metadata": metadata}
	if spec != nil {
		obj["spec"] = spec
	}
	return &unstructured.Unstructured{Object: obj}
}

func newCertificateObject(namespace string, spec, status map[string]any) *unstructured.Unstructured {
	obj := map[string]any{
		"apiVersion": "cert-manager.io/v1",
		"kind":       "Certificate",
		"metadata":   map[string]any{"namespace": namespace, "name": "site"},
	}
	if spec != nil {
		obj["spec"] = spec
	}
	if status != nil {
		obj["status"] = status
	}
	return &unstructured.Unstructured{Object: obj}
}

func syncOne(t *testing.T, sync func(*Source, context.Context, chan<- source.Data) error, s *Source) ([]source.Data, error) {
	t.Helper()
	results := make(chan source.Data, 20)
	err := sync(s, t.Context(), results)
	close(results)
	return collectData(results), err
}

func TestSyncIngressRoutes(t *testing.T) {
	setupFixedTime(t)

	full := newIngressRouteObject("team-a", "web", map[string]any{"app": "web"}, map[string]any{
		"entryPoints": []any{"web", "websecure"},
		"routes": []any{
			map[string]any{
				"match": "Host(`web.example.com`)",
				"services": []any{
					map[string]any{"name": "web-svc", "kind": "Service", "namespace": "team-a", "port": int64(8080)},
					map[string]any{"name": "named-port", "port": "http"},
					map[string]any{"name": "no-port"},
				},
				"middlewares": []any{
					map[string]any{"name": "auth", "namespace": "team-a"},
					"not-a-map",
				},
			},
			map[string]any{"match": "PathPrefix(`/api`)"},
		},
		"tls": map[string]any{"secretName": "web-tls", "certResolver": "le", "options": map[string]any{"name": "strict"}},
	})
	bare := newIngressRouteObject("team-b", "api", nil, nil)

	s := newCRDSource(t, allServed(), full, bare)
	items, err := syncOne(t, (*Source).syncIngressRoutes, s)
	require.NoError(t, err)
	require.Len(t, items, 2)

	byNamespace := map[any]map[string]any{}
	for _, item := range items {
		assert.Equal(t, ingressRouteType, item.Type)
		assert.Equal(t, source.DataOperationUpsert, item.Operation)
		assert.Equal(t, testFixedTime, item.Time)
		byNamespace[item.Values["namespace"]] = item.Values
	}
	require.Contains(t, byNamespace, "team-a")
	require.Contains(t, byNamespace, "team-b")

	assert.Equal(t, map[string]any{
		"apiServer":   testAPIServer,
		"name":        "web",
		"namespace":   "team-a",
		"labels":      map[string]string{"app": "web"},
		"entryPoints": []string{"web", "websecure"},
		"routes": []map[string]any{
			{
				"match": "Host(`web.example.com`)",
				"services": []map[string]any{
					{"name": "web-svc", "kind": "Service", "namespace": "team-a", "port": int64(8080)},
					{"name": "named-port", "kind": "", "namespace": "", "port": "http"},
					{"name": "no-port", "kind": "", "namespace": "", "port": ""},
				},
				"middlewares": []map[string]any{{"name": "auth", "namespace": "team-a"}},
			},
			{"match": "PathPrefix(`/api`)", "services": []map[string]any{}, "middlewares": []map[string]any{}},
		},
		"tls": map[string]any{"secretName": "web-tls", "certResolver": "le", "options": "strict"},
	}, byNamespace["team-a"])

	assert.Equal(t, map[string]any{
		"apiServer":   testAPIServer,
		"name":        "api",
		"namespace":   "team-b",
		"labels":      map[string]string{},
		"entryPoints": []string{},
		"routes":      []map[string]any{},
		"tls":         map[string]any{},
	}, byNamespace["team-b"])
}

func TestIngressRouteValuesOddFields(t *testing.T) {
	t.Parallel()

	values := ingressRouteValues(testAPIServer, map[string]any{
		"metadata": map[string]any{"name": "odd", "labels": "nope"},
		"spec": map[string]any{
			"entryPoints": "web",
			"routes":      []any{map[string]any{"match": 12, "services": "x", "middlewares": 3}},
			"tls":         map[string]any{"options": "not-a-map", "secretName": 5},
		},
	})

	assert.Equal(t, map[string]string{}, values["labels"])
	assert.Equal(t, []string{}, values["entryPoints"])
	assert.Equal(t, []map[string]any{{"match": "", "services": []map[string]any{}, "middlewares": []map[string]any{}}}, values["routes"])
	assert.Equal(t, map[string]any{"secretName": "", "certResolver": "", "options": ""}, values["tls"])
	assert.Empty(t, portField(map[string]any{"port": []any{}}))
}

func TestSyncCertificates(t *testing.T) {
	setupFixedTime(t)

	ready := newCertificateObject("team-a", map[string]any{
		"dnsNames":  []any{"a.example.com", "b.example.com"},
		"issuerRef": map[string]any{"name": "letsencrypt", "kind": "ClusterIssuer", "group": "cert-manager.io"},
	}, map[string]any{
		"renewalTime": "2025-03-01T00:00:00Z",
		"notAfter":    "2025-04-01T00:00:00Z",
		"conditions": []any{
			map[string]any{"type": "Issuing", "status": "False", "reason": "Other"},
			map[string]any{"type": "Ready", "status": "True", "reason": "Ready"},
		},
	})
	notReady := newCertificateObject("team-b", nil, map[string]any{
		"conditions": []any{map[string]any{"type": "Ready", "status": "False", "reason": "Pending"}},
	})
	noCondition := newCertificateObject("team-c", nil, nil)
	emptyStatus := newCertificateObject("team-d", nil, map[string]any{
		"conditions": []any{map[string]any{"type": "Ready"}},
	})

	s := newCRDSource(t, allServed(), ready, notReady, noCondition, emptyStatus)
	items, err := syncOne(t, (*Source).syncCertificates, s)
	require.NoError(t, err)
	require.Len(t, items, 4)

	byNamespace := map[any]map[string]any{}
	for _, item := range items {
		assert.Equal(t, certificateType, item.Type)
		assert.Equal(t, testFixedTime, item.Time)
		byNamespace[item.Values["namespace"]] = item.Values
	}

	assert.Equal(t, map[string]any{
		"apiServer":    testAPIServer,
		"name":         "site",
		"namespace":    "team-a",
		"labels":       map[string]string{},
		"dnsNames":     []string{"a.example.com", "b.example.com"},
		"issuer":       map[string]any{"name": "letsencrypt", "kind": "ClusterIssuer", "group": "cert-manager.io"},
		"status":       "True",
		"statusReason": "Ready",
		"renewalTime":  "2025-03-01T00:00:00Z",
		"notAfter":     "2025-04-01T00:00:00Z",
	}, byNamespace["team-a"])

	assert.Equal(t, map[string]any{
		"apiServer":    testAPIServer,
		"name":         "site",
		"namespace":    "team-b",
		"labels":       map[string]string{},
		"dnsNames":     []string{},
		"issuer":       map[string]any{"name": "", "kind": "", "group": ""},
		"status":       "False",
		"statusReason": "Pending",
		"renewalTime":  "",
		"notAfter":     "",
	}, byNamespace["team-b"])

	assert.Equal(t, "Unknown", byNamespace["team-c"]["status"])
	assert.Empty(t, byNamespace["team-c"]["statusReason"])
	assert.Equal(t, "Unknown", byNamespace["team-d"]["status"])
}

func TestResolveGVR(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		served   []*metav1.APIResourceList
		kind     crdKind
		expected schema.GroupVersionResource
		found    bool
	}{
		"traefik v1alpha1": {
			served:   []*metav1.APIResourceList{serverResources("traefik.io/v1alpha1", ingressRoutesRes)},
			kind:     ingressRouteKind,
			expected: traefikV1Alpha1,
			found:    true,
		},
		"traefik discovery preferred version wins": {
			served: []*metav1.APIResourceList{
				serverResources("traefik.io/v3", ingressRoutesRes),
				serverResources("traefik.io/v1alpha1", ingressRoutesRes),
			},
			kind:     ingressRouteKind,
			expected: traefikV3,
			found:    true,
		},
		"traefik falls back when preferred version lacks the resource": {
			served: []*metav1.APIResourceList{
				serverResources("traefik.io/v3", "middlewares"),
				serverResources("traefik.io/v1alpha1", ingressRoutesRes),
			},
			kind:     ingressRouteKind,
			expected: traefikV1Alpha1,
			found:    true,
		},
		"legacy traefik group is not supported": {
			served: []*metav1.APIResourceList{serverResources("traefik.containo.us/v1alpha1", ingressRoutesRes)},
			kind:   ingressRouteKind,
		},
		"group served without the resource": {
			served: []*metav1.APIResourceList{serverResources("traefik.io/v1alpha1", "middlewares")},
			kind:   ingressRouteKind,
		},
		"no groups": {kind: certificateKind},
		"certmanager prefers v1": {
			served: []*metav1.APIResourceList{
				serverResources("cert-manager.io/v1beta1", certificatesRes),
				serverResources("cert-manager.io/v1", certificatesRes),
			},
			kind:     certificateKind,
			expected: certManagerV1,
			found:    true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := newCRDSource(t, tc.served)
			gvr, found, err := s.resolveGVR(t.Context(), tc.kind)
			require.NoError(t, err)
			assert.Equal(t, tc.found, found)
			assert.Equal(t, tc.expected, gvr)
		})
	}
}

func TestResolveGVRVersionNotFoundIsSkipped(t *testing.T) {
	t.Parallel()

	// the preferred version answers NotFound for its resource list: the next version is used.
	s := newCRDSource(t, []*metav1.APIResourceList{
		serverResources("traefik.io/v3", ingressRoutesRes),
		serverResources("traefik.io/v1alpha1", ingressRoutesRes),
	})
	discovery, ok := s.clientset.Discovery().(*fakediscovery.FakeDiscovery)
	require.True(t, ok)
	calls := 0
	discovery.PrependReactor("get", "resource", func(clienttesting.Action) (bool, runtime.Object, error) {
		calls++
		if calls == 1 {
			return true, nil, apierrors.NewNotFound(schema.GroupResource{}, "v3")
		}
		return false, nil, nil
	})

	gvr, found, err := s.resolveGVR(t.Context(), ingressRouteKind)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, traefikV1Alpha1, gvr)
}

func TestSyncCRDNotInstalled(t *testing.T) {
	setupFixedTime(t)

	for _, kind := range []struct {
		name string
		sync func(*Source, context.Context, chan<- source.Data) error
	}{
		{name: "ingressroute", sync: (*Source).syncIngressRoutes},
		{name: "certificate", sync: (*Source).syncCertificates},
	} {
		t.Run(kind.name, func(t *testing.T) {
			s := newCRDSource(t, nil)
			items, err := syncOne(t, kind.sync, s)
			require.NoError(t, err)
			assert.Empty(t, items)
		})
	}
}

func TestStartSyncProcessCRDs(t *testing.T) {
	setupFixedTime(t)

	route := newIngressRouteObject("team-a", "web", nil, nil)
	cert := newCertificateObject("team-a", nil, nil)

	testCases := map[string]struct {
		served []*metav1.APIResourceList
		types  map[string]source.Extra
		expect []string
	}{
		"both in order": {
			served: allServed(),
			types:  map[string]source.Extra{certificateType: {}, ingressRouteType: {}, namespaceType: {}},
			expect: []string{namespaceType, ingressRouteType, certificateType},
		},
		"traefik not installed does not affect others": {
			served: []*metav1.APIResourceList{serverResources("cert-manager.io/v1", certificatesRes)},
			types:  map[string]source.Extra{certificateType: {}, ingressRouteType: {}, namespaceType: {}},
			expect: []string{namespaceType, certificateType},
		},
		"nothing installed": {
			types:  map[string]source.Extra{certificateType: {}, ingressRouteType: {}, namespaceType: {}},
			expect: []string{namespaceType},
		},
		"unknown types skipped": {
			served: allServed(),
			types:  map[string]source.Extra{"helmrelease": {}},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			s := newCRDSource(t, tc.served, route, cert)
			s.clientset = withNamespace(t, s.clientset)
			results := make(chan source.Data, 10)

			require.NoError(t, s.StartSyncProcess(t.Context(), tc.types, results))
			close(results)

			got := make([]string, 0, len(tc.expect))
			for _, item := range collectData(results) {
				got = append(got, item.Type)
			}
			assert.ElementsMatch(t, tc.expect, got)
			if len(tc.expect) > 1 {
				assert.Equal(t, tc.expect, got)
			}
		})
	}
}

// withNamespace adds one namespace to the fake clientset, keeping its fake discovery.
func withNamespace(t *testing.T, clientset kubernetes.Interface) kubernetes.Interface {
	t.Helper()
	_, err := clientset.CoreV1().Namespaces().Create(t.Context(), newNamespace("team-a", nil), metav1.CreateOptions{})
	require.NoError(t, err)
	return clientset
}

func TestStartSyncProcessCRDErrorIsolation(t *testing.T) {
	setupFixedTime(t)

	t.Run("list error on one type", func(t *testing.T) {
		route := newIngressRouteObject("team-a", "web", nil, nil)
		cert := newCertificateObject("team-a", nil, nil)
		s := newCRDSource(t, allServed(), route, cert)
		fakeDyn, ok := s.dynamic.(*dynamicfake.FakeDynamicClient)
		require.True(t, ok)
		fakeDyn.PrependReactor("list", ingressRoutesRes, func(clienttesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("forbidden")
		})

		results := make(chan source.Data, 10)
		err := s.StartSyncProcess(t.Context(), map[string]source.Extra{ingressRouteType: {}, certificateType: {}}, results)
		require.ErrorIs(t, err, ErrK8sSource)
		require.ErrorIs(t, err, ErrRetrievingAssets)
		close(results)

		items := collectData(results)
		require.Len(t, items, 1)
		assert.Equal(t, certificateType, items[0].Type)
	})

	t.Run("discovery error", func(t *testing.T) {
		s := newCRDSource(t, allServed(), newCertificateObject("team-a", nil, nil))
		discovery, ok := s.clientset.Discovery().(*fakediscovery.FakeDiscovery)
		require.True(t, ok)
		discovery.PrependReactor("get", "group", func(clienttesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("discovery down")
		})

		results := make(chan source.Data, 10)
		err := s.StartSyncProcess(t.Context(), map[string]source.Extra{ingressRouteType: {}, certificateType: {}}, results)
		require.ErrorIs(t, err, ErrRetrievingAssets)
		close(results)
		assert.Empty(t, collectData(results))
	})

	t.Run("group version discovery error", func(t *testing.T) {
		s := newCRDSource(t, allServed())
		discovery, ok := s.clientset.Discovery().(*fakediscovery.FakeDiscovery)
		require.True(t, ok)
		discovery.PrependReactor("get", "resource", func(clienttesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("boom")
		})

		_, err := syncOne(t, (*Source).syncCertificates, s)
		require.ErrorIs(t, err, ErrRetrievingAssets)
	})
}

func TestSyncCRDContextCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	s := newCRDSource(t, allServed(), newCertificateObject("team-a", nil, nil))
	require.ErrorIs(t, s.syncCertificates(ctx, make(chan source.Data, 1)), context.Canceled)
}

func TestSyncCRDCanceledWhileSending(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	s := newCRDSource(t, allServed(), newCertificateObject("team-a", nil, nil))

	done := make(chan error, 1)
	go func() { done <- s.syncCertificates(ctx, make(chan source.Data)) }()
	cancel()

	require.ErrorIs(t, <-done, context.Canceled)
}

// newCRDHTTPSource creates a Source whose real clientset and dynamic client talk to an httptest server.
func newCRDHTTPSource(t *testing.T, handler http.Handler) *Source {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	config := &rest.Config{Host: server.URL}
	clientset, err := kubernetes.NewForConfig(config)
	require.NoError(t, err)
	dynamicClient, err := dynamic.NewForConfig(config)
	require.NoError(t, err)

	return &Source{apiServer: testAPIServer, clusterName: testClusterName, clientset: clientset, dynamic: dynamicClient}
}

func TestSyncCRDPagination(t *testing.T) {
	setupFixedTime(t)

	var continues []string
	s := newCRDHTTPSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/apis":
			writeJSON(t, w, metav1.APIGroupList{
				TypeMeta: metav1.TypeMeta{Kind: "APIGroupList", APIVersion: "v1"},
				Groups: []metav1.APIGroup{{
					Name:             certManagerGroup,
					Versions:         []metav1.GroupVersionForDiscovery{{GroupVersion: "cert-manager.io/v1", Version: "v1"}},
					PreferredVersion: metav1.GroupVersionForDiscovery{GroupVersion: "cert-manager.io/v1", Version: "v1"},
				}},
			})
		case "/apis/cert-manager.io/v1":
			writeJSON(t, w, serverResources("cert-manager.io/v1", certificatesRes))
		case "/apis/cert-manager.io/v1/certificates":
			continues = append(continues, r.URL.Query().Get("continue"))
			assert.Equal(t, "500", r.URL.Query().Get("limit"))
			item := func(name string) map[string]any {
				return map[string]any{"metadata": map[string]any{"name": name, "namespace": "team-a"}}
			}
			if r.URL.Query().Get("continue") == "" {
				writeJSON(t, w, map[string]any{
					"apiVersion": "cert-manager.io/v1", "kind": "CertificateList",
					"metadata": map[string]any{"continue": "page-2"},
					"items":    []any{item("first")},
				})
				return
			}
			writeJSON(t, w, map[string]any{
				"apiVersion": "cert-manager.io/v1", "kind": "CertificateList",
				"metadata": map[string]any{},
				"items":    []any{item("second")},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	items, err := syncOne(t, (*Source).syncCertificates, s)
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, "first", items[0].Values["name"])
	assert.Equal(t, "second", items[1].Values["name"])
	assert.Equal(t, []string{"", "page-2"}, continues)
}

func TestSyncCRDDiscoveryHTTP(t *testing.T) {
	t.Parallel()

	s := newCRDHTTPSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/apis" {
			writeJSON(t, w, metav1.APIGroupList{Groups: []metav1.APIGroup{}})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))

	// no group served: skipped
	items, err := syncOne(t, (*Source).syncCertificates, s)
	require.NoError(t, err)
	assert.Empty(t, items)

	// discovery failing on the groups endpoint is a real error
	broken := newCRDHTTPSource(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	_, err = syncOne(t, (*Source).syncCertificates, broken)
	require.ErrorIs(t, err, ErrRetrievingAssets)
}
