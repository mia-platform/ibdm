// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package k8s

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/mia-platform/ibdm/internal/source"
)

func TestSyncCluster(t *testing.T) {
	setupFixedTime(t)

	testCases := map[string]struct {
		objects   []runtime.Object
		nodeCount int
		nodes     []map[string]any
		totalCPU  float64
	}{
		"no nodes": {nodeCount: 0, nodes: []map[string]any{}},
		"multiple nodes": {
			objects:   []runtime.Object{newNodePtr("node-3"), newNodePtr("node-1"), newNodePtr("node-2")},
			nodeCount: 3,
			nodes: []map[string]any{
				expectedNode("node-1", 0, []map[string]any{}),
				expectedNode("node-2", 0, []map[string]any{}),
				expectedNode("node-3", 0, []map[string]any{}),
			},
		},
		"details, taints and fractional cpu": {
			objects: []runtime.Object{
				detailedNode("node-b", "500m", nil),
				detailedNode("node-a", "4", []corev1.Taint{
					{Key: "dedicated", Value: "gpu", Effect: corev1.TaintEffectNoSchedule},
					{Key: "node.kubernetes.io/unschedulable", Effect: corev1.TaintEffectNoExecute},
				}),
				newNodePtr("node-c"),
			},
			nodeCount: 3,
			nodes: []map[string]any{
				expectedNode("node-a", 4, []map[string]any{
					{"key": "dedicated", "value": "gpu", "effect": "NoSchedule"},
					{"key": "node.kubernetes.io/unschedulable", "value": "", "effect": "NoExecute"},
				}),
				expectedNode("node-b", 0.5, []map[string]any{}),
				expectedNode("node-c", 0, []map[string]any{}),
			},
			totalCPU: 4.5,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			s := newFakeSource(t, tc.objects...)
			results := make(chan source.Data, 5)

			require.NoError(t, s.syncCluster(t.Context(), results))
			close(results)

			items := collectData(results)
			require.Len(t, items, 1)
			assert.Equal(t, clusterType, items[0].Type)
			assert.Equal(t, source.DataOperationUpsert, items[0].Operation)
			assert.Equal(t, testFixedTime, items[0].Time)
			assert.Equal(t, map[string]any{
				"apiServer":     testAPIServer,
				"clusterName":   testClusterName,
				"nodeCount":     tc.nodeCount,
				"nodes":         tc.nodes,
				"totalCPUCores": tc.totalCPU,
			}, items[0].Values)
		})
	}
}

func TestSyncClusterListError(t *testing.T) {
	t.Parallel()

	s := newHTTPSource(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	results := make(chan source.Data, 1)

	require.ErrorIs(t, s.syncCluster(t.Context(), results), ErrRetrievingAssets)
	assert.Empty(t, results)
}

// detailedNode builds a node with fictional system info, CPU capacity and taints.
func detailedNode(name, cpu string, taints []corev1.Taint) *corev1.Node {
	node := newNodePtr(name)
	node.Spec.Taints = taints
	node.Status.Capacity = corev1.ResourceList{corev1.ResourceCPU: resource.MustParse(cpu)}
	node.Status.NodeInfo = corev1.NodeSystemInfo{
		KubeletVersion: "v1.30.0",
		OSImage:        "Example Linux 1.0",
		KernelVersion:  "6.1.0-example",
		Architecture:   "amd64",
	}
	return node
}

// expectedNode builds the expected per-node map; nodes created with newNodePtr have no system info.
func expectedNode(name string, cpu float64, taints []map[string]any) map[string]any {
	info := map[string]any{
		"name": name, "kubeletVersion": "", "osImage": "", "kernelVersion": "", "architecture": "",
		"cpuCapacity": cpu, "taints": taints,
	}
	if cpu > 0 {
		info["kubeletVersion"] = "v1.30.0"
		info["osImage"] = "Example Linux 1.0"
		info["kernelVersion"] = "6.1.0-example"
		info["architecture"] = "amd64"
	}
	return info
}

func newNodePtr(name string) *corev1.Node {
	node := newNode(name)
	return &node
}

func TestSyncNamespaces(t *testing.T) {
	setupFixedTime(t)

	s := newFakeSource(t,
		newNamespace("team-a", map[string]string{"env": "prod"}),
		newNamespace("team-b", nil),
	)
	results := make(chan source.Data, 5)

	require.NoError(t, s.syncNamespaces(t.Context(), results))
	close(results)

	items := collectData(results)
	require.Len(t, items, 2)

	for _, item := range items {
		assert.Equal(t, namespaceType, item.Type)
		assert.Equal(t, source.DataOperationUpsert, item.Operation)
		assert.Equal(t, testFixedTime, item.Time)
		assert.Equal(t, testAPIServer, item.Values["apiServer"])
	}

	first, ok := items[0].Values["namespace"].(map[string]any)
	require.True(t, ok)
	metadata, ok := first["metadata"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "team-a", metadata["name"])
	assert.Equal(t, map[string]any{"env": "prod"}, metadata["labels"])
	assert.NotContains(t, metadata, "managedFields")

	second, ok := items[1].Values["namespace"].(map[string]any)
	require.True(t, ok)
	secondMetadata, ok := second["metadata"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "team-b", secondMetadata["name"])
	assert.NotContains(t, secondMetadata, "labels")
}

func TestSyncNamespacesPagination(t *testing.T) {
	setupFixedTime(t)

	s := newHTTPSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/namespaces", r.URL.Path)
		if r.URL.Query().Get("continue") == "" {
			writeJSON(t, w, corev1.NamespaceList{
				ListMeta: metav1.ListMeta{Continue: "page-2"},
				Items:    []corev1.Namespace{*newNamespace("team-a", nil)},
			})
			return
		}
		writeJSON(t, w, corev1.NamespaceList{Items: []corev1.Namespace{*newNamespace("team-b", nil)}})
	}))
	results := make(chan source.Data, 5)

	require.NoError(t, s.syncNamespaces(t.Context(), results))
	close(results)
	assert.Len(t, collectData(results), 2)
}

func TestSyncNamespacesListError(t *testing.T) {
	t.Parallel()

	s := newHTTPSource(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))

	require.ErrorIs(t, s.syncNamespaces(t.Context(), make(chan source.Data, 1)), ErrRetrievingAssets)
}

func TestSyncNamespacesContextCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	s := newFakeSource(t, newNamespace("team-a", nil))
	require.ErrorIs(t, s.syncNamespaces(ctx, make(chan source.Data)), context.Canceled)
}

func TestSyncNamespacesCanceledWhileSending(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	s := newFakeSource(t, newNamespace("team-a", nil))

	// unbuffered channel with no reader: the send blocks until the context is canceled.
	done := make(chan error, 1)
	go func() { done <- s.syncNamespaces(ctx, make(chan source.Data)) }()
	cancel()

	require.ErrorIs(t, <-done, context.Canceled)
}

func TestListAllNodesPagination(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	s := newHTTPSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/nodes", r.URL.Path)
		assert.Equal(t, strconv.FormatInt(pageSize, 10), r.URL.Query().Get("limit"))
		calls.Add(1)

		switch r.URL.Query().Get("continue") {
		case "":
			writeJSON(t, w, corev1.NodeList{
				ListMeta: metav1.ListMeta{Continue: "page-2"},
				Items:    []corev1.Node{newNode("node-1"), newNode("node-2")},
			})
		case "page-2":
			writeJSON(t, w, corev1.NodeList{Items: []corev1.Node{newNode("node-3")}})
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))

	nodes, err := s.listAllNodes(t.Context())
	require.NoError(t, err)
	require.Len(t, nodes, 3)
	assert.Equal(t, "node-1", nodes[0].Name)
	assert.Equal(t, "node-3", nodes[2].Name)
	assert.EqualValues(t, 2, calls.Load())
}

func TestListAllNodesEmpty(t *testing.T) {
	t.Parallel()

	s := newFakeSource(t)
	nodes, err := s.listAllNodes(t.Context())
	require.NoError(t, err)
	assert.Empty(t, nodes)
}

func TestListAllNodesError(t *testing.T) {
	t.Parallel()

	s := newHTTPSource(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))

	_, err := s.listAllNodes(t.Context())
	require.ErrorIs(t, err, ErrRetrievingAssets)
}

func TestListAllNodesContextCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	s := newFakeSource(t)
	_, err := s.listAllNodes(ctx)
	require.ErrorIs(t, err, ctx.Err())
}

func newService(namespace, name string, labels map[string]string, spec corev1.ServiceSpec) *corev1.Service {
	return &corev1.Service{ObjectMeta: testMeta(namespace, name, labels), Spec: spec}
}

func syncOneService(t *testing.T, service *corev1.Service) map[string]any {
	t.Helper()
	results := make(chan source.Data, 2)
	require.NoError(t, newFakeSource(t, service).syncServices(t.Context(), results))
	close(results)

	items := collectData(results)
	require.Len(t, items, 1)
	return items[0].Values
}

func TestSyncServicesEnvelope(t *testing.T) {
	setupFixedTime(t)

	s := newFakeSource(t,
		newService("team-a", "web", map[string]string{"app": "web"}, corev1.ServiceSpec{}),
		newService("team-b", "web", nil, corev1.ServiceSpec{}),
	)
	results := make(chan source.Data, 5)

	require.NoError(t, s.syncServices(t.Context(), results))
	close(results)

	items := collectData(results)
	require.Len(t, items, 2)
	byNamespace := map[any]map[string]any{}
	for _, item := range items {
		assert.Equal(t, serviceType, item.Type)
		assert.Equal(t, source.DataOperationUpsert, item.Operation)
		assert.Equal(t, testFixedTime, item.Time)
		assert.Equal(t, testAPIServer, item.Values["apiServer"])
		byNamespace[item.Values["namespace"]] = item.Values
	}
	require.Contains(t, byNamespace, "team-a")
	require.Contains(t, byNamespace, "team-b")
	assert.Equal(t, "web", byNamespace["team-a"]["name"])
	assert.Equal(t, map[string]string{"app": "web"}, byNamespace["team-a"]["labels"])
	assert.Equal(t, map[string]string{}, byNamespace["team-b"]["labels"])
}

func TestSyncServicesValues(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		service *corev1.Service
		expect  map[string]any
	}{
		"empty spec uses defaults and never nil": {
			service: newService("ns", "svc", nil, corev1.ServiceSpec{}),
			expect: map[string]any{
				"type":         "ClusterIP",
				"clusterIPs":   []string{},
				"ports":        []map[string]any{},
				"loadBalancer": []map[string]any{},
				"externalIPs":  []string{},
				"externalName": "",
				"selector":     map[string]string{},
			},
		},
		"clusterip with clusterIP fallback and port defaults": {
			service: newService("ns", "svc", nil, corev1.ServiceSpec{
				Type:      corev1.ServiceTypeClusterIP,
				ClusterIP: "10.0.0.10",
				Selector:  map[string]string{"app": "web"},
				Ports:     []corev1.ServicePort{{Name: "http", Port: 80}},
			}),
			expect: map[string]any{
				"type":       "ClusterIP",
				"clusterIPs": []string{"10.0.0.10"},
				"selector":   map[string]string{"app": "web"},
				"ports": []map[string]any{
					{"name": "http", "protocol": "TCP", "port": 80, "targetPort": 80, "nodePort": 0},
				},
			},
		},
		"clusterIPs preferred over clusterIP": {
			service: newService("ns", "svc", nil, corev1.ServiceSpec{
				ClusterIP:  "10.0.0.10",
				ClusterIPs: []string{"10.0.0.10", "fd00::10"},
			}),
			expect: map[string]any{"clusterIPs": []string{"10.0.0.10", "fd00::10"}},
		},
		"headless keeps None": {
			service: newService("ns", "svc", nil, corev1.ServiceSpec{ClusterIP: "None", ClusterIPs: []string{"None"}}),
			expect:  map[string]any{"clusterIPs": []string{"None"}, "type": "ClusterIP"},
		},
		"headless None from clusterIP only": {
			service: newService("ns", "svc", nil, corev1.ServiceSpec{ClusterIP: "None"}),
			expect:  map[string]any{"clusterIPs": []string{"None"}},
		},
		"nodeport with int and named target ports": {
			service: newService("ns", "svc", nil, corev1.ServiceSpec{
				Type: corev1.ServiceTypeNodePort,
				Ports: []corev1.ServicePort{
					{Name: "http", Protocol: corev1.ProtocolTCP, Port: 80, TargetPort: intstr.FromInt32(8080), NodePort: 30080},
					{Name: "dns", Protocol: corev1.ProtocolUDP, Port: 53, TargetPort: intstr.FromString("dns-udp"), NodePort: 30053},
					{Name: "unset", Port: 9000, TargetPort: intstr.FromString("")},
				},
			}),
			expect: map[string]any{
				"type": "NodePort",
				"ports": []map[string]any{
					{"name": "http", "protocol": "TCP", "port": 80, "targetPort": 8080, "nodePort": 30080},
					{"name": "dns", "protocol": "UDP", "port": 53, "targetPort": "dns-udp", "nodePort": 30053},
					{"name": "unset", "protocol": "TCP", "port": 9000, "targetPort": 9000, "nodePort": 0},
				},
			},
		},
		"externalname": {
			service: newService("ns", "svc", nil, corev1.ServiceSpec{
				Type:         corev1.ServiceTypeExternalName,
				ExternalName: "db.example.com",
			}),
			expect: map[string]any{"type": "ExternalName", "externalName": "db.example.com", "clusterIPs": []string{}},
		},
		"external ips": {
			service: newService("ns", "svc", nil, corev1.ServiceSpec{ExternalIPs: []string{"192.0.2.10"}}),
			expect:  map[string]any{"externalIPs": []string{"192.0.2.10"}},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			values := syncOneService(t, tc.service)
			for key, expected := range tc.expect {
				assert.Equal(t, expected, values[key], key)
			}
			// never nil, whatever the case
			assert.NotNil(t, values["labels"])
			assert.NotNil(t, values["clusterIPs"])
			assert.NotNil(t, values["ports"])
			assert.NotNil(t, values["loadBalancer"])
			assert.NotNil(t, values["externalIPs"])
			assert.NotNil(t, values["selector"])
		})
	}
}

func TestSyncServicesLoadBalancer(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		ingress []corev1.LoadBalancerIngress
		expect  []map[string]any
	}{
		"ip":       {ingress: []corev1.LoadBalancerIngress{{IP: "203.0.113.5"}}, expect: []map[string]any{{"ip": "203.0.113.5", "hostname": ""}}},
		"hostname": {ingress: []corev1.LoadBalancerIngress{{Hostname: "lb.example.com"}}, expect: []map[string]any{{"ip": "", "hostname": "lb.example.com"}}},
		"pending":  {expect: []map[string]any{}},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			service := newService("ns", "lb", nil, corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer})
			service.Status.LoadBalancer.Ingress = tc.ingress

			values := syncOneService(t, service)
			assert.Equal(t, "LoadBalancer", values["type"])
			assert.Equal(t, tc.expect, values["loadBalancer"])
		})
	}
}

func TestSyncServicesEmpty(t *testing.T) {
	t.Parallel()

	results := make(chan source.Data, 1)
	require.NoError(t, newFakeSource(t).syncServices(t.Context(), results))
	assert.Empty(t, results)
}

func TestSyncServicesPagination(t *testing.T) {
	setupFixedTime(t)

	var limits []string
	s := newHTTPSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/services", r.URL.Path)
		limits = append(limits, r.URL.Query().Get("limit"))
		if r.URL.Query().Get("continue") == "" {
			writeJSON(t, w, corev1.ServiceList{
				ListMeta: metav1.ListMeta{Continue: "page-2"},
				Items:    []corev1.Service{*newService("team-a", "one", nil, corev1.ServiceSpec{})},
			})
			return
		}
		assert.Equal(t, "page-2", r.URL.Query().Get("continue"))
		writeJSON(t, w, corev1.ServiceList{Items: []corev1.Service{*newService("team-b", "two", nil, corev1.ServiceSpec{})}})
	}))
	results := make(chan source.Data, 5)

	require.NoError(t, s.syncServices(t.Context(), results))
	close(results)

	assert.Len(t, collectData(results), 2)
	assert.Equal(t, []string{"500", "500"}, limits)
}

func TestSyncServicesListError(t *testing.T) {
	t.Parallel()

	s := newHTTPSource(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	require.ErrorIs(t, s.syncServices(t.Context(), make(chan source.Data, 1)), ErrRetrievingAssets)
}

func TestSyncServicesContextCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	s := newFakeSource(t, newService("team-a", "web", nil, corev1.ServiceSpec{}))
	require.ErrorIs(t, s.syncServices(ctx, make(chan source.Data)), context.Canceled)
}

func TestSyncServicesCanceledWhileSending(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	s := newFakeSource(t, newService("team-a", "web", nil, corev1.ServiceSpec{}))

	done := make(chan error, 1)
	go func() { done <- s.syncServices(ctx, make(chan source.Data)) }()
	cancel()

	require.ErrorIs(t, <-done, context.Canceled)
}

func TestStartSyncProcessServiceDispatchOrder(t *testing.T) {
	setupFixedTime(t)

	pod := testPodSpec(nil, nil)
	s := newFakeSource(t,
		newNamespace("team-a", nil),
		newDeployment("team-a", "web", nil, nil, pod),
		newService("team-a", "web", nil, corev1.ServiceSpec{}),
	)
	results := make(chan source.Data, 10)

	types := map[string]source.Extra{serviceType: {}, deploymentType: {}, namespaceType: {}, "unknown": {}}
	require.NoError(t, s.StartSyncProcess(t.Context(), types, results))
	close(results)

	items := collectData(results)
	gotTypes := make([]string, 0, len(items))
	for _, item := range items {
		gotTypes = append(gotTypes, item.Type)
	}
	assert.Equal(t, []string{namespaceType, deploymentType, serviceType}, gotTypes)
}

func TestStartSyncProcessServiceErrorIsolation(t *testing.T) {
	setupFixedTime(t)

	// services are forbidden, namespaces are still served.
	s := newHTTPSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/namespaces" {
			writeJSON(t, w, corev1.NamespaceList{Items: []corev1.Namespace{*newNamespace("team-a", nil)}})
			return
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	results := make(chan source.Data, 10)

	err := s.StartSyncProcess(t.Context(), map[string]source.Extra{namespaceType: {}, serviceType: {}}, results)
	require.ErrorIs(t, err, ErrK8sSource)
	require.ErrorIs(t, err, ErrRetrievingAssets)
	close(results)

	items := collectData(results)
	require.Len(t, items, 1)
	assert.Equal(t, namespaceType, items[0].Type)
}

func testPodSpec(containers, initContainers []corev1.Container) corev1.PodSpec {
	return corev1.PodSpec{Containers: containers, InitContainers: initContainers}
}

func testContainer(name, image string) corev1.Container {
	return corev1.Container{Name: name, Image: image}
}

func testMeta(namespace, name string, labels map[string]string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Namespace: namespace, Name: name, Labels: labels}
}

func newDeployment(namespace, name string, labels map[string]string, replicas *int32, pod corev1.PodSpec) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: testMeta(namespace, name, labels),
		Spec:       appsv1.DeploymentSpec{Replicas: replicas, Template: corev1.PodTemplateSpec{Spec: pod}},
	}
}

func newStatefulSet(namespace, name string, labels map[string]string, replicas *int32, pod corev1.PodSpec) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: testMeta(namespace, name, labels),
		Spec:       appsv1.StatefulSetSpec{Replicas: replicas, Template: corev1.PodTemplateSpec{Spec: pod}},
	}
}

func newDaemonSet(namespace, name string, labels map[string]string, pod corev1.PodSpec) *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		ObjectMeta: testMeta(namespace, name, labels),
		Spec:       appsv1.DaemonSetSpec{Template: corev1.PodTemplateSpec{Spec: pod}},
	}
}

func int32Ptr(v int32) *int32 { return &v }

// workloadKind describes one workload kind for table-driven tests.
type workloadKind struct {
	dataType    string
	hasReplicas bool
	sync        func(s *Source, ctx context.Context, results chan<- source.Data) error
	path        string
	// build creates an object of this kind.
	build func(namespace, name string, labels map[string]string, replicas *int32, pod corev1.PodSpec) runtime.Object
	// list creates a list response of this kind holding the given objects.
	list func(continueToken string, objects ...runtime.Object) any
}

func workloadKinds() map[string]workloadKind {
	return map[string]workloadKind{
		"deployment": {
			dataType:    deploymentType,
			hasReplicas: true,
			sync:        (*Source).syncDeployments,
			path:        "/apis/apps/v1/deployments",
			build: func(namespace, name string, labels map[string]string, replicas *int32, pod corev1.PodSpec) runtime.Object {
				return newDeployment(namespace, name, labels, replicas, pod)
			},
			list: func(token string, objects ...runtime.Object) any {
				list := appsv1.DeploymentList{ListMeta: metav1.ListMeta{Continue: token}}
				for _, o := range objects {
					list.Items = append(list.Items, *o.(*appsv1.Deployment)) //nolint:forcetypeassert // test helper
				}
				return list
			},
		},
		"statefulset": {
			dataType:    statefulSetType,
			hasReplicas: true,
			sync:        (*Source).syncStatefulSets,
			path:        "/apis/apps/v1/statefulsets",
			build: func(namespace, name string, labels map[string]string, replicas *int32, pod corev1.PodSpec) runtime.Object {
				return newStatefulSet(namespace, name, labels, replicas, pod)
			},
			list: func(token string, objects ...runtime.Object) any {
				list := appsv1.StatefulSetList{ListMeta: metav1.ListMeta{Continue: token}}
				for _, o := range objects {
					list.Items = append(list.Items, *o.(*appsv1.StatefulSet)) //nolint:forcetypeassert // test helper
				}
				return list
			},
		},
		"daemonset": {
			dataType: daemonSetType,
			sync:     (*Source).syncDaemonSets,
			path:     "/apis/apps/v1/daemonsets",
			build: func(namespace, name string, labels map[string]string, _ *int32, pod corev1.PodSpec) runtime.Object {
				return newDaemonSet(namespace, name, labels, pod)
			},
			list: func(token string, objects ...runtime.Object) any {
				list := appsv1.DaemonSetList{ListMeta: metav1.ListMeta{Continue: token}}
				for _, o := range objects {
					list.Items = append(list.Items, *o.(*appsv1.DaemonSet)) //nolint:forcetypeassert // test helper
				}
				return list
			},
		},
	}
}

func TestSyncWorkloads(t *testing.T) {
	setupFixedTime(t)

	for kindName, kind := range workloadKinds() {
		t.Run(kindName, func(t *testing.T) {
			pod := testPodSpec(
				[]corev1.Container{testContainer("web", "nginx:1.27"), testContainer("sidecar", "proxy:2")},
				[]corev1.Container{testContainer("init-b", "busybox:1"), testContainer("init-a", "busybox:2")},
			)
			s := newFakeSource(t,
				kind.build("team-a", "app", map[string]string{"env": "prod"}, int32Ptr(3), pod),
				kind.build("team-b", "app", nil, nil, testPodSpec(nil, nil)),
			)
			results := make(chan source.Data, 5)

			require.NoError(t, kind.sync(s, t.Context(), results))
			close(results)

			items := collectData(results)
			require.Len(t, items, 2)
			for _, item := range items {
				assert.Equal(t, kind.dataType, item.Type)
				assert.Equal(t, source.DataOperationUpsert, item.Operation)
				assert.Equal(t, testFixedTime, item.Time)
				assert.Equal(t, testAPIServer, item.Values["apiServer"])
			}

			byNamespace := map[any]map[string]any{}
			for _, item := range items {
				byNamespace[item.Values["namespace"]] = item.Values
			}
			require.Contains(t, byNamespace, "team-a")
			require.Contains(t, byNamespace, "team-b")

			full := byNamespace["team-a"]
			assert.Equal(t, "app", full["name"])
			assert.Equal(t, map[string]string{"env": "prod"}, full["labels"])
			assert.Equal(t, []map[string]any{
				{"name": "web", "image": "nginx:1.27", "resources": emptyResources()},
				{"name": "sidecar", "image": "proxy:2", "resources": emptyResources()},
			}, full["containers"])
			assert.Equal(t, []map[string]any{
				{"name": "init-b", "image": "busybox:1"},
				{"name": "init-a", "image": "busybox:2"},
			}, full["initContainers"])

			empty := byNamespace["team-b"]
			assert.Equal(t, map[string]string{}, empty["labels"])
			assert.Equal(t, []map[string]any{}, empty["containers"])
			assert.Equal(t, []map[string]any{}, empty["initContainers"])

			if kind.hasReplicas {
				assert.Equal(t, 3, full["replicas"])
				assert.Equal(t, 1, empty["replicas"])
			} else {
				assert.NotContains(t, full, "replicas")
				assert.NotContains(t, empty, "replicas")
			}
		})
	}
}

func emptyResources() map[string]any {
	return map[string]any{"requests": map[string]string{}, "limits": map[string]string{}}
}

func TestSyncWorkloadsContainerResources(t *testing.T) {
	setupFixedTime(t)

	for kindName, kind := range workloadKinds() {
		t.Run(kindName, func(t *testing.T) {
			both := testContainer("both", "img:1")
			both.Resources = corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("500m"),
					corev1.ResourceMemory: resource.MustParse("256Mi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("1Gi"),
				},
			}
			onlyRequests := testContainer("requests", "img:2")
			onlyRequests.Resources.Requests = corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")}
			none := testContainer("none", "img:3")
			extended := testContainer("extended", "img:4")
			extended.Resources.Limits = corev1.ResourceList{
				"nvidia.com/gpu":                resource.MustParse("2"),
				corev1.ResourceEphemeralStorage: resource.MustParse("2Gi"),
				"hugepages-2Mi":                 resource.MustParse("128Mi"),
			}
			canonical := testContainer("canonical", "img:5")
			canonical.Resources.Requests = corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1000m"),
				corev1.ResourceMemory: resource.MustParse("0.5"),
			}
			canonical.Resources.Limits = corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("0.5")}

			initContainer := testContainer("init", "busybox:1")
			initContainer.Resources.Requests = corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")}

			pod := testPodSpec(
				[]corev1.Container{both, onlyRequests, none, extended, canonical},
				[]corev1.Container{initContainer},
			)
			s := newFakeSource(t, kind.build("team-a", "app", nil, nil, pod))
			results := make(chan source.Data, 2)

			require.NoError(t, kind.sync(s, t.Context(), results))
			close(results)

			items := collectData(results)
			require.Len(t, items, 1)
			assert.Equal(t, []map[string]any{
				{"name": "both", "image": "img:1", "resources": map[string]any{
					"requests": map[string]string{"cpu": "500m", "memory": "256Mi"},
					"limits":   map[string]string{"cpu": "1", "memory": "1Gi"},
				}},
				{"name": "requests", "image": "img:2", "resources": map[string]any{
					"requests": map[string]string{"cpu": "100m"},
					"limits":   map[string]string{},
				}},
				{"name": "none", "image": "img:3", "resources": emptyResources()},
				{"name": "extended", "image": "img:4", "resources": map[string]any{
					"requests": map[string]string{},
					"limits":   map[string]string{"nvidia.com/gpu": "2", "ephemeral-storage": "2Gi", "hugepages-2Mi": "128Mi"},
				}},
				{"name": "canonical", "image": "img:5", "resources": map[string]any{
					"requests": map[string]string{"cpu": "1", "memory": "500m"},
					"limits":   map[string]string{"cpu": "500m"},
				}},
			}, items[0].Values["containers"])
			assert.Equal(t, []map[string]any{
				{"name": "init", "image": "busybox:1"},
			}, items[0].Values["initContainers"])
		})
	}
}

func TestSyncWorkloadsEmpty(t *testing.T) {
	t.Parallel()

	for kindName, kind := range workloadKinds() {
		t.Run(kindName, func(t *testing.T) {
			t.Parallel()

			results := make(chan source.Data, 1)
			require.NoError(t, kind.sync(newFakeSource(t), t.Context(), results))
			assert.Empty(t, results)
		})
	}
}

func TestSyncWorkloadsPagination(t *testing.T) {
	setupFixedTime(t)

	for kindName, kind := range workloadKinds() {
		t.Run(kindName, func(t *testing.T) {
			var limits []string
			s := newHTTPSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, kind.path, r.URL.Path)
				limits = append(limits, r.URL.Query().Get("limit"))
				if r.URL.Query().Get("continue") == "" {
					writeJSON(t, w, kind.list("page-2", kind.build("team-a", "one", nil, nil, testPodSpec(nil, nil))))
					return
				}
				assert.Equal(t, "page-2", r.URL.Query().Get("continue"))
				writeJSON(t, w, kind.list("", kind.build("team-b", "two", nil, nil, testPodSpec(nil, nil))))
			}))
			results := make(chan source.Data, 5)

			require.NoError(t, kind.sync(s, t.Context(), results))
			close(results)

			assert.Len(t, collectData(results), 2)
			assert.Equal(t, []string{"500", "500"}, limits)
		})
	}
}

func TestSyncWorkloadsListError(t *testing.T) {
	t.Parallel()

	for kindName, kind := range workloadKinds() {
		t.Run(kindName, func(t *testing.T) {
			t.Parallel()

			s := newHTTPSource(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusForbidden)
			}))
			require.ErrorIs(t, kind.sync(s, t.Context(), make(chan source.Data, 1)), ErrRetrievingAssets)
		})
	}
}

func TestSyncWorkloadsContextCanceled(t *testing.T) {
	t.Parallel()

	for kindName, kind := range workloadKinds() {
		t.Run(kindName, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			cancel()

			s := newFakeSource(t, kind.build("team-a", "app", nil, nil, testPodSpec(nil, nil)))
			require.ErrorIs(t, kind.sync(s, ctx, make(chan source.Data)), context.Canceled)
		})
	}
}

func TestSyncWorkloadsCanceledWhileSending(t *testing.T) {
	t.Parallel()

	for kindName, kind := range workloadKinds() {
		t.Run(kindName, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			s := newFakeSource(t, kind.build("team-a", "app", nil, nil, testPodSpec(nil, nil)))

			// unbuffered channel with no reader: the send blocks until the context is canceled.
			done := make(chan error, 1)
			go func() { done <- kind.sync(s, ctx, make(chan source.Data)) }()
			cancel()

			require.ErrorIs(t, <-done, context.Canceled)
		})
	}
}

func TestStartSyncProcessWorkloadsDispatch(t *testing.T) {
	setupFixedTime(t)

	pod := testPodSpec(nil, nil)
	s := newFakeSource(t,
		newDeployment("team-a", "web", nil, nil, pod),
		newStatefulSet("team-a", "db", nil, nil, pod),
		newDaemonSet("team-a", "agent", nil, pod),
	)
	results := make(chan source.Data, 10)

	types := map[string]source.Extra{deploymentType: {}, statefulSetType: {}, daemonSetType: {}, "unknown": {}}
	require.NoError(t, s.StartSyncProcess(t.Context(), types, results))
	close(results)

	items := collectData(results)
	gotTypes := make([]string, 0, len(items))
	for _, item := range items {
		gotTypes = append(gotTypes, item.Type)
	}
	assert.Equal(t, []string{deploymentType, statefulSetType, daemonSetType}, gotTypes)
}

func TestStartSyncProcessWorkloadsErrorIsolation(t *testing.T) {
	setupFixedTime(t)

	// statefulsets are forbidden, the other workloads are served.
	s := newHTTPSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		kinds := workloadKinds()
		switch r.URL.Path {
		case kinds["deployment"].path:
			writeJSON(t, w, kinds["deployment"].list("", newDeployment("team-a", "web", nil, nil, testPodSpec(nil, nil))))
		case kinds["daemonset"].path:
			writeJSON(t, w, kinds["daemonset"].list("", newDaemonSet("team-a", "agent", nil, testPodSpec(nil, nil))))
		default:
			w.WriteHeader(http.StatusForbidden)
		}
	}))
	results := make(chan source.Data, 10)

	types := map[string]source.Extra{deploymentType: {}, statefulSetType: {}, daemonSetType: {}}
	err := s.StartSyncProcess(t.Context(), types, results)
	require.ErrorIs(t, err, ErrK8sSource)
	require.ErrorIs(t, err, ErrRetrievingAssets)
	close(results)

	items := collectData(results)
	require.Len(t, items, 2)
	assert.Equal(t, deploymentType, items[0].Type)
	assert.Equal(t, daemonSetType, items[1].Type)
}

var networkPoliciesGVR = networkingv1.SchemeGroupVersion.WithResource("networkpolicies")

func newNetworkPolicy(namespace, name string, labels map[string]string, spec networkingv1.NetworkPolicySpec) *networkingv1.NetworkPolicy {
	return &networkingv1.NetworkPolicy{ObjectMeta: testMeta(namespace, name, labels), Spec: spec}
}

func syncOneNetworkPolicy(t *testing.T, policy *networkingv1.NetworkPolicy) map[string]any {
	t.Helper()
	results := make(chan source.Data, 2)
	require.NoError(t, newFakeSource(t, policy).syncNetworkPolicies(t.Context(), results))
	close(results)

	items := collectData(results)
	require.Len(t, items, 1)
	return items[0].Values
}

func protocolPtr(protocol corev1.Protocol) *corev1.Protocol { return &protocol }

func portPtr(port intstr.IntOrString) *intstr.IntOrString { return &port }

// emptySelector is the rendered form of a selector that selects everything.
func emptySelector() map[string]any {
	return map[string]any{"matchLabels": map[string]string{}, "matchExpressions": []map[string]any{}}
}

func TestSyncNetworkPoliciesEnvelope(t *testing.T) {
	setupFixedTime(t)

	s := newFakeSource(t,
		newNetworkPolicy("team-a", "deny", map[string]string{"app": "web"}, networkingv1.NetworkPolicySpec{}),
		newNetworkPolicy("team-b", "deny", nil, networkingv1.NetworkPolicySpec{}),
	)
	results := make(chan source.Data, 5)

	require.NoError(t, s.syncNetworkPolicies(t.Context(), results))
	close(results)

	items := collectData(results)
	require.Len(t, items, 2)
	byNamespace := map[any]map[string]any{}
	for _, item := range items {
		assert.Equal(t, networkPolicyType, item.Type)
		assert.Equal(t, source.DataOperationUpsert, item.Operation)
		assert.Equal(t, testFixedTime, item.Time)
		assert.Equal(t, testAPIServer, item.Values["apiServer"])
		assert.Equal(t, "deny", item.Values["name"])
		byNamespace[item.Values["namespace"]] = item.Values
	}
	require.Contains(t, byNamespace, "team-a")
	require.Contains(t, byNamespace, "team-b")
	assert.Equal(t, map[string]string{"app": "web"}, byNamespace["team-a"]["labels"])
	assert.Equal(t, map[string]string{}, byNamespace["team-b"]["labels"])
}

func TestNetworkPolicyValuesPodSelector(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		selector metav1.LabelSelector
		want     map[string]any
	}{
		"empty selects all": {selector: metav1.LabelSelector{}, want: emptySelector()},
		"match labels": {
			selector: metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}},
			want:     map[string]any{"matchLabels": map[string]string{"app": "web"}, "matchExpressions": []map[string]any{}},
		},
		"match expressions": {
			selector: metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
				{Key: "tier", Operator: metav1.LabelSelectorOpIn, Values: []string{"frontend", "backend"}},
				{Key: "debug", Operator: metav1.LabelSelectorOpDoesNotExist},
			}},
			want: map[string]any{
				"matchLabels": map[string]string{},
				"matchExpressions": []map[string]any{
					{"key": "tier", "operator": "In", "values": []string{"frontend", "backend"}},
					{"key": "debug", "operator": "DoesNotExist", "values": []string{}},
				},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			values := networkPolicyValues(newNetworkPolicy("ns", "p", nil, networkingv1.NetworkPolicySpec{PodSelector: tc.selector}), testAPIServer)
			assert.Equal(t, tc.want, values["podSelector"])
		})
	}
}

func TestNetworkPolicyValuesPolicyTypes(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		spec networkingv1.NetworkPolicySpec
		want []string
	}{
		"explicit": {
			spec: networkingv1.NetworkPolicySpec{PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress}},
			want: []string{"Egress"},
		},
		"explicit both": {
			spec: networkingv1.NetworkPolicySpec{PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress}},
			want: []string{"Ingress", "Egress"},
		},
		"defaulted ingress only": {
			spec: networkingv1.NetworkPolicySpec{Ingress: []networkingv1.NetworkPolicyIngressRule{{}}},
			want: []string{"Ingress"},
		},
		"defaulted without rules": {spec: networkingv1.NetworkPolicySpec{}, want: []string{"Ingress"}},
		"defaulted ingress and egress": {
			spec: networkingv1.NetworkPolicySpec{Egress: []networkingv1.NetworkPolicyEgressRule{{}}},
			want: []string{"Ingress", "Egress"},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			values := networkPolicyValues(newNetworkPolicy("ns", "p", nil, tc.spec), testAPIServer)
			assert.Equal(t, tc.want, values["policyTypes"])
		})
	}
}

func TestSyncNetworkPoliciesDefaultDenyVersusAllowAll(t *testing.T) {
	t.Parallel()

	deny := syncOneNetworkPolicy(t, newNetworkPolicy("ns", "deny", nil, networkingv1.NetworkPolicySpec{
		PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress},
	}))
	assert.Equal(t, []map[string]any{}, deny["ingress"])
	assert.Equal(t, []map[string]any{}, deny["egress"])

	allow := syncOneNetworkPolicy(t, newNetworkPolicy("ns", "allow", nil, networkingv1.NetworkPolicySpec{
		PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress},
		Ingress:     []networkingv1.NetworkPolicyIngressRule{{}},
		Egress:      []networkingv1.NetworkPolicyEgressRule{{}},
	}))
	assert.Equal(t, []map[string]any{{"from": []map[string]any{}, "ports": []map[string]any{}}}, allow["ingress"])
	assert.Equal(t, []map[string]any{{"to": []map[string]any{}, "ports": []map[string]any{}}}, allow["egress"])
}

func TestNetworkPolicyValuesPeers(t *testing.T) {
	t.Parallel()

	web := &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}}
	webValues := map[string]any{"matchLabels": map[string]string{"app": "web"}, "matchExpressions": []map[string]any{}}

	tests := map[string]struct {
		peer networkingv1.NetworkPolicyPeer
		want map[string]any
	}{
		"pod selector": {
			peer: networkingv1.NetworkPolicyPeer{PodSelector: web},
			want: map[string]any{"podSelector": webValues},
		},
		"namespace selector": {
			peer: networkingv1.NetworkPolicyPeer{NamespaceSelector: web},
			want: map[string]any{"namespaceSelector": webValues},
		},
		"empty namespace selector is kept": {
			peer: networkingv1.NetworkPolicyPeer{NamespaceSelector: &metav1.LabelSelector{}},
			want: map[string]any{"namespaceSelector": emptySelector()},
		},
		"empty pod selector is kept": {
			peer: networkingv1.NetworkPolicyPeer{PodSelector: &metav1.LabelSelector{}},
			want: map[string]any{"podSelector": emptySelector()},
		},
		"both selectors": {
			peer: networkingv1.NetworkPolicyPeer{PodSelector: web, NamespaceSelector: &metav1.LabelSelector{}},
			want: map[string]any{"podSelector": webValues, "namespaceSelector": emptySelector()},
		},
		"ip block with except": {
			peer: networkingv1.NetworkPolicyPeer{IPBlock: &networkingv1.IPBlock{CIDR: "10.0.0.0/8", Except: []string{"10.1.0.0/16"}}},
			want: map[string]any{"ipBlock": map[string]any{"cidr": "10.0.0.0/8", "except": []string{"10.1.0.0/16"}}},
		},
		"ip block without except": {
			peer: networkingv1.NetworkPolicyPeer{IPBlock: &networkingv1.IPBlock{CIDR: "192.0.2.0/24"}},
			want: map[string]any{"ipBlock": map[string]any{"cidr": "192.0.2.0/24", "except": []string{}}},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			spec := networkingv1.NetworkPolicySpec{
				Ingress: []networkingv1.NetworkPolicyIngressRule{{From: []networkingv1.NetworkPolicyPeer{tc.peer}}},
				Egress:  []networkingv1.NetworkPolicyEgressRule{{To: []networkingv1.NetworkPolicyPeer{tc.peer}}},
			}
			values := networkPolicyValues(newNetworkPolicy("ns", "p", nil, spec), testAPIServer)
			assert.Equal(t, []map[string]any{{"from": []map[string]any{tc.want}, "ports": []map[string]any{}}}, values["ingress"])
			assert.Equal(t, []map[string]any{{"to": []map[string]any{tc.want}, "ports": []map[string]any{}}}, values["egress"])
		})
	}
}

func TestNetworkPolicyValuesPorts(t *testing.T) {
	t.Parallel()

	endPort := int32(32768)
	tests := map[string]struct {
		port networkingv1.NetworkPolicyPort
		want map[string]any
	}{
		"numeric": {
			port: networkingv1.NetworkPolicyPort{Protocol: protocolPtr(corev1.ProtocolTCP), Port: portPtr(intstr.FromInt32(8080))},
			want: map[string]any{"protocol": "TCP", "port": 8080},
		},
		"named": {
			port: networkingv1.NetworkPolicyPort{Protocol: protocolPtr(corev1.ProtocolUDP), Port: portPtr(intstr.FromString("dns"))},
			want: map[string]any{"protocol": "UDP", "port": "dns"},
		},
		"numeric looking name stays a string": {
			port: networkingv1.NetworkPolicyPort{Port: portPtr(intstr.FromString("12345"))},
			want: map[string]any{"protocol": "TCP", "port": "12345"},
		},
		"protocol default": {
			port: networkingv1.NetworkPolicyPort{Port: portPtr(intstr.FromInt32(443))},
			want: map[string]any{"protocol": "TCP", "port": 443},
		},
		"empty protocol default": {
			port: networkingv1.NetworkPolicyPort{Protocol: protocolPtr(""), Port: portPtr(intstr.FromInt32(443))},
			want: map[string]any{"protocol": "TCP", "port": 443},
		},
		"end port": {
			port: networkingv1.NetworkPolicyPort{Protocol: protocolPtr(corev1.ProtocolSCTP), Port: portPtr(intstr.FromInt32(32000)), EndPort: &endPort},
			want: map[string]any{"protocol": "SCTP", "port": 32000, "endPort": 32768},
		},
		"protocol only omits port": {
			port: networkingv1.NetworkPolicyPort{Protocol: protocolPtr(corev1.ProtocolUDP)},
			want: map[string]any{"protocol": "UDP"},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			spec := networkingv1.NetworkPolicySpec{
				Ingress: []networkingv1.NetworkPolicyIngressRule{{Ports: []networkingv1.NetworkPolicyPort{tc.port}}},
			}
			values := networkPolicyValues(newNetworkPolicy("ns", "p", nil, spec), testAPIServer)
			assert.Equal(t, []map[string]any{{"from": []map[string]any{}, "ports": []map[string]any{tc.want}}}, values["ingress"])
		})
	}
}

func TestSyncNetworkPoliciesEmpty(t *testing.T) {
	t.Parallel()

	results := make(chan source.Data, 1)
	require.NoError(t, newFakeSource(t).syncNetworkPolicies(t.Context(), results))
	assert.Empty(t, results)
}

func TestSyncNetworkPoliciesPagination(t *testing.T) {
	setupFixedTime(t)

	var limits []string
	s := newHTTPSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/apis/networking.k8s.io/v1/networkpolicies", r.URL.Path)
		limits = append(limits, r.URL.Query().Get("limit"))
		if r.URL.Query().Get("continue") == "" {
			writeJSON(t, w, networkingv1.NetworkPolicyList{
				ListMeta: metav1.ListMeta{Continue: "page-2"},
				Items:    []networkingv1.NetworkPolicy{*newNetworkPolicy("team-a", "one", nil, networkingv1.NetworkPolicySpec{})},
			})
			return
		}
		assert.Equal(t, "page-2", r.URL.Query().Get("continue"))
		writeJSON(t, w, networkingv1.NetworkPolicyList{
			Items: []networkingv1.NetworkPolicy{*newNetworkPolicy("team-b", "two", nil, networkingv1.NetworkPolicySpec{})},
		})
	}))
	results := make(chan source.Data, 5)

	require.NoError(t, s.syncNetworkPolicies(t.Context(), results))
	close(results)

	assert.Len(t, collectData(results), 2)
	assert.Equal(t, []string{"500", "500"}, limits)
}

func TestSyncNetworkPoliciesListError(t *testing.T) {
	t.Parallel()

	s := newHTTPSource(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	require.ErrorIs(t, s.syncNetworkPolicies(t.Context(), make(chan source.Data, 1)), ErrRetrievingAssets)
}

func TestSyncNetworkPoliciesContextCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	s := newFakeSource(t, newNetworkPolicy("team-a", "deny", nil, networkingv1.NetworkPolicySpec{}))
	require.ErrorIs(t, s.syncNetworkPolicies(ctx, make(chan source.Data)), context.Canceled)
}

func TestSyncNetworkPoliciesCanceledWhileSending(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	s := newFakeSource(t, newNetworkPolicy("team-a", "deny", nil, networkingv1.NetworkPolicySpec{}))

	done := make(chan error, 1)
	go func() { done <- s.syncNetworkPolicies(ctx, make(chan source.Data)) }()
	cancel()

	require.ErrorIs(t, <-done, context.Canceled)
}

func TestStartSyncProcessNetworkPolicyDispatchOrder(t *testing.T) {
	setupFixedTime(t)

	s := newFakeSource(t,
		newNamespace("team-a", nil),
		newService("team-a", "web", nil, corev1.ServiceSpec{}),
		newNetworkPolicy("team-a", "deny", nil, networkingv1.NetworkPolicySpec{}),
	)
	results := make(chan source.Data, 10)

	types := map[string]source.Extra{networkPolicyType: {}, serviceType: {}, namespaceType: {}, "unknown": {}}
	require.NoError(t, s.StartSyncProcess(t.Context(), types, results))
	close(results)

	items := collectData(results)
	gotTypes := make([]string, 0, len(items))
	for _, item := range items {
		gotTypes = append(gotTypes, item.Type)
	}
	assert.Equal(t, []string{namespaceType, serviceType, networkPolicyType}, gotTypes)
}

func TestStartSyncProcessNetworkPolicyErrorIsolation(t *testing.T) {
	setupFixedTime(t)

	// network policies are forbidden, namespaces are still served.
	s := newHTTPSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/namespaces" {
			writeJSON(t, w, corev1.NamespaceList{Items: []corev1.Namespace{*newNamespace("team-a", nil)}})
			return
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	results := make(chan source.Data, 10)

	err := s.StartSyncProcess(t.Context(), map[string]source.Extra{namespaceType: {}, networkPolicyType: {}}, results)
	require.ErrorIs(t, err, ErrK8sSource)
	require.ErrorIs(t, err, ErrRetrievingAssets)
	close(results)

	items := collectData(results)
	require.Len(t, items, 1)
	assert.Equal(t, namespaceType, items[0].Type)
}

func TestStartEventStreamNetworkPolicySnapshot(t *testing.T) {
	harness := startStream(t, []string{networkPolicyType},
		newNetworkPolicy("team-a", "deny", nil, networkingv1.NetworkPolicySpec{}),
		newNetworkPolicy("team-b", "allow", nil, networkingv1.NetworkPolicySpec{Ingress: []networkingv1.NetworkPolicyIngressRule{{}}}),
	)

	byNamespace := map[any]source.Data{}
	for _, data := range harness.nextN(2) {
		assert.Equal(t, networkPolicyType, data.Type)
		assert.Equal(t, source.DataOperationUpsert, data.Operation)
		assert.Equal(t, testFixedTime, data.Time)
		byNamespace[data.Values["namespace"]] = data
	}
	assert.Equal(t, []map[string]any{}, byNamespace["team-a"].Values["ingress"])
	assert.Len(t, byNamespace["team-b"].Values["ingress"], 1)
	harness.waitWatches(1)
}

func TestStartEventStreamNetworkPolicyLiveChanges(t *testing.T) {
	harness := startStream(t, []string{networkPolicyType})
	harness.waitWatches(1)

	build := func(version string, annotations map[string]string, spec networkingv1.NetworkPolicySpec) runtime.Object {
		obj := newNetworkPolicy("team-a", "web", nil, spec)
		obj.ResourceVersion = version
		obj.Annotations = annotations
		return obj
	}
	kind := findEventKind(t, networkPolicyType)
	valuesOf := func(obj runtime.Object) map[string]any {
		values, err := kind.values(obj, testAPIServer)
		require.NoError(t, err)
		return values
	}

	created := build("1", nil, networkingv1.NetworkPolicySpec{})
	harness.create(networkPoliciesGVR, created, "team-a")
	assert.Equal(t,
		source.Data{Type: networkPolicyType, Operation: source.DataOperationUpsert, Values: valuesOf(created), Time: testFixedTime},
		harness.next())

	// a metadata-only update emits nothing: the next item is the rule change that follows it.
	harness.update(networkPoliciesGVR, build("2", map[string]string{"note": "churn"}, networkingv1.NetworkPolicySpec{}), "team-a")
	changed := build("3", map[string]string{"note": "churn"}, networkingv1.NetworkPolicySpec{
		Ingress: []networkingv1.NetworkPolicyIngressRule{{Ports: []networkingv1.NetworkPolicyPort{{Port: portPtr(intstr.FromInt32(80))}}}},
	})
	harness.update(networkPoliciesGVR, changed, "team-a")
	data := harness.next()
	assert.Equal(t, source.DataOperationUpsert, data.Operation)
	assert.Equal(t, valuesOf(changed), data.Values)

	harness.remove(networkPoliciesGVR, "team-a", "web")
	data = harness.next()
	assert.Equal(t, source.DataOperationDelete, data.Operation)
	assert.Equal(t, networkPolicyType, data.Type)
	assert.Equal(t, valuesOf(changed), data.Values)
}

func TestEventHandlersNetworkPolicyNoOpUpdates(t *testing.T) {
	setupFixedTime(t)

	results := make(chan source.Data, 10)
	stream := &eventStream{ctx: t.Context(), log: nilLogger(t), apiServer: testAPIServer, results: results}
	handler := stream.handlers(findEventKind(t, networkPolicyType))

	old := versioned(newNetworkPolicy("team-a", "web", nil, networkingv1.NetworkPolicySpec{}), "1")

	same := newNetworkPolicy("team-a", "web", nil, networkingv1.NetworkPolicySpec{})
	same.Annotations = map[string]string{"note": "not sent"}
	handler.OnUpdate(old, versioned(same, "2"))
	assert.Empty(t, results)

	changed := newNetworkPolicy("team-a", "web", nil, networkingv1.NetworkPolicySpec{
		Egress: []networkingv1.NetworkPolicyEgressRule{{}},
	})
	handler.OnUpdate(old, versioned(changed, "2"))
	require.Len(t, results, 1)
	assert.Equal(t, []string{"Ingress", "Egress"}, (<-results).Values["policyTypes"])
}

// helmPayload builds Helm's storage format: JSON, gzip, base64.
func helmPayload(t *testing.T, release map[string]any, compress bool) []byte {
	t.Helper()
	raw, err := json.Marshal(release)
	require.NoError(t, err)

	if compress {
		var buf bytes.Buffer
		writer := gzip.NewWriter(&buf)
		_, err = writer.Write(raw)
		require.NoError(t, err)
		require.NoError(t, writer.Close())
		raw = buf.Bytes()
	}
	return []byte(base64.StdEncoding.EncodeToString(raw))
}

func testRelease(namespace, name string, revision int) map[string]any {
	return map[string]any{
		"name":      name,
		"namespace": namespace,
		"version":   revision,
		"info": map[string]any{
			"status":         "deployed",
			"first_deployed": "2024-01-01T00:00:00Z",
			"last_deployed":  "2024-02-01T00:00:00Z",
		},
		"chart": map[string]any{
			"metadata": map[string]any{"name": "web-chart", "version": "1.2.3", "appVersion": "1.20"},
			"files":    []any{map[string]any{"name": "ignored", "data": "c2VjcmV0"}},
		},
		"manifest": "kind: Secret",
		"config":   map[string]any{"password": "ignored"},
	}
}

func newHelmSecret(namespace, name string, revision int, status string, data []byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "sh.helm.release.v1." + name + ".v" + strconv.Itoa(revision),
			Labels: map[string]string{
				"owner":   "helm",
				"name":    name,
				"status":  status,
				"version": strconv.Itoa(revision),
			},
		},
		Data: map[string][]byte{"release": data},
	}
}

func syncHelm(t *testing.T, s *Source) []source.Data {
	t.Helper()
	results := make(chan source.Data, 20)
	require.NoError(t, s.syncHelmReleases(t.Context(), results))
	close(results)
	return collectData(results)
}

func TestSyncHelmReleases(t *testing.T) {
	setupFixedTime(t)

	s := newFakeSource(t,
		newHelmSecret("team-a", "web", 3, "deployed", helmPayload(t, testRelease("team-a", "web", 3), true)),
		newHelmSecret("team-a", "web", 2, "superseded", helmPayload(t, testRelease("team-a", "web", 2), true)),
		newHelmSecret("team-b", "api", 1, "deployed", helmPayload(t, testRelease("team-b", "api", 1), false)),
		newHelmSecret("team-b", "broken", 1, "failed", helmPayload(t, testRelease("team-b", "broken", 1), true)),
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "other"}, Data: map[string][]byte{"release": []byte("x")}},
	)

	items := syncHelm(t, s)
	require.Len(t, items, 2)

	assert.Equal(t, helmReleaseType, items[0].Type)
	assert.Equal(t, source.DataOperationUpsert, items[0].Operation)
	assert.Equal(t, testFixedTime, items[0].Time)
	assert.Equal(t, map[string]any{
		"apiServer":     testAPIServer,
		"name":          "web",
		"namespace":     "team-a",
		"revision":      3,
		"status":        "deployed",
		"chartName":     "web-chart",
		"chartVersion":  "1.2.3",
		"appVersion":    "1.20",
		"firstDeployed": "2024-01-01T00:00:00Z",
		"lastDeployed":  "2024-02-01T00:00:00Z",
	}, items[0].Values)
	assert.Equal(t, "api", items[1].Values["name"])
	assert.Equal(t, "team-b", items[1].Values["namespace"])
}

func TestSyncHelmReleasesEmpty(t *testing.T) {
	t.Parallel()
	assert.Empty(t, syncHelm(t, newFakeSource(t)))
}

func TestSyncHelmReleasesMissingChartFields(t *testing.T) {
	t.Parallel()

	release := map[string]any{"name": "web", "namespace": "team-a", "version": 1}
	s := newFakeSource(t, newHelmSecret("team-a", "web", 1, "deployed", helmPayload(t, release, true)))

	items := syncHelm(t, s)
	require.Len(t, items, 1)
	values := items[0].Values
	for _, key := range []string{"status", "chartName", "chartVersion", "appVersion", "firstDeployed", "lastDeployed"} {
		assert.Empty(t, values[key], key)
	}
	assert.Equal(t, 1, values["revision"])
}

func TestSyncHelmReleasesNonStringDates(t *testing.T) {
	t.Parallel()

	release := testRelease("team-a", "web", 1)
	info, ok := release["info"].(map[string]any)
	require.True(t, ok)
	info["first_deployed"] = map[string]any{"seconds": 1}
	info["last_deployed"] = nil
	s := newFakeSource(t, newHelmSecret("team-a", "web", 1, "deployed", helmPayload(t, release, true)))

	items := syncHelm(t, s)
	require.Len(t, items, 1)
	assert.Empty(t, items[0].Values["firstDeployed"])
	assert.Empty(t, items[0].Values["lastDeployed"])
	assert.Equal(t, "deployed", items[0].Values["status"])
}

func TestSyncHelmReleasesCorruptSecretsSkipped(t *testing.T) {
	t.Parallel()

	goodGzip := helmPayload(t, testRelease("team-a", "web", 1), true)
	badGzip := base64.StdEncoding.EncodeToString(append([]byte{0x1f, 0x8b}, []byte("not gzip")...))

	s := newFakeSource(t,
		newHelmSecret("team-a", "web", 1, "deployed", goodGzip),
		newHelmSecret("team-a", "bad-base64", 1, "deployed", []byte("%%% not base64")),
		newHelmSecret("team-a", "bad-gzip", 1, "deployed", []byte(badGzip)),
		newHelmSecret("team-a", "bad-json", 1, "deployed", []byte(base64.StdEncoding.EncodeToString([]byte("{not json")))),
		newHelmSecret("team-a", "no-data", 1, "deployed", nil),
	)

	items := syncHelm(t, s)
	require.Len(t, items, 1)
	assert.Equal(t, "web", items[0].Values["name"])
}

func TestSyncHelmReleasesDuplicateHighestRevision(t *testing.T) {
	t.Parallel()

	low := testRelease("team-a", "web", 2)
	high := testRelease("team-a", "web", 5)
	high["chart"] = map[string]any{"metadata": map[string]any{"name": "web-chart", "version": "9.9.9"}}

	s := newFakeSource(t,
		newHelmSecret("team-a", "web", 5, "deployed", helmPayload(t, high, true)),
		newHelmSecret("team-a", "web-copy", 2, "deployed", helmPayload(t, low, true)),
	)

	items := syncHelm(t, s)
	require.Len(t, items, 1)
	assert.Equal(t, 5, items[0].Values["revision"])
	assert.Equal(t, "9.9.9", items[0].Values["chartVersion"])
}

func TestSyncHelmReleasesFallbacks(t *testing.T) {
	t.Parallel()

	s := newFakeSource(t, newHelmSecret("team-a", "web", 4, "deployed", helmPayload(t, map[string]any{}, true)))

	items := syncHelm(t, s)
	require.Len(t, items, 1)
	assert.Equal(t, "web", items[0].Values["name"])
	assert.Equal(t, "team-a", items[0].Values["namespace"])
	assert.Equal(t, 4, items[0].Values["revision"])
}

func TestSyncHelmReleasesPagination(t *testing.T) {
	setupFixedTime(t)

	var selectors, limits []string
	payload := helmPayload(t, testRelease("team-a", "web", 1), true)
	s := newHTTPSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/secrets", r.URL.Path)
		selectors = append(selectors, r.URL.Query().Get("labelSelector"))
		limits = append(limits, r.URL.Query().Get("limit"))
		if r.URL.Query().Get("continue") == "" {
			writeJSON(t, w, corev1.SecretList{
				ListMeta: metav1.ListMeta{Continue: "page-2"},
				Items:    []corev1.Secret{*newHelmSecret("team-a", "web", 1, "deployed", payload)},
			})
			return
		}
		assert.Equal(t, "page-2", r.URL.Query().Get("continue"))
		writeJSON(t, w, corev1.SecretList{Items: []corev1.Secret{
			*newHelmSecret("team-b", "api", 1, "deployed", helmPayload(t, testRelease("team-b", "api", 1), true)),
		}})
	}))

	results := make(chan source.Data, 5)
	require.NoError(t, s.syncHelmReleases(t.Context(), results))
	close(results)

	assert.Len(t, collectData(results), 2)
	assert.Equal(t, []string{"500", "500"}, limits)
	assert.Equal(t, []string{helmSecretSelector, helmSecretSelector}, selectors)
}

func TestSyncHelmReleasesListError(t *testing.T) {
	t.Parallel()

	s := newHTTPSource(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	require.ErrorIs(t, s.syncHelmReleases(t.Context(), make(chan source.Data, 1)), ErrRetrievingAssets)
}

func TestSyncHelmReleasesContextCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	s := newFakeSource(t, newHelmSecret("team-a", "web", 1, "deployed", helmPayload(t, testRelease("team-a", "web", 1), true)))
	require.ErrorIs(t, s.syncHelmReleases(ctx, make(chan source.Data)), context.Canceled)
}

func TestSyncHelmReleasesCanceledWhileSending(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	s := newFakeSource(t, newHelmSecret("team-a", "web", 1, "deployed", helmPayload(t, testRelease("team-a", "web", 1), true)))

	done := make(chan error, 1)
	go func() { done <- s.syncHelmReleases(ctx, make(chan source.Data)) }()
	cancel()

	require.ErrorIs(t, <-done, context.Canceled)
}

func TestStartSyncProcessHelmReleaseDispatchOrder(t *testing.T) {
	setupFixedTime(t)

	s := newFakeSource(t,
		newNamespace("team-a", nil),
		newService("team-a", "web", nil, corev1.ServiceSpec{}),
		newHelmSecret("team-a", "web", 1, "deployed", helmPayload(t, testRelease("team-a", "web", 1), true)),
	)
	results := make(chan source.Data, 10)

	types := map[string]source.Extra{helmReleaseType: {}, serviceType: {}, namespaceType: {}, "unknown": {}}
	require.NoError(t, s.StartSyncProcess(t.Context(), types, results))
	close(results)

	collected := collectData(results)
	got := make([]string, 0, len(collected))
	for _, item := range collected {
		got = append(got, item.Type)
	}
	assert.Equal(t, []string{namespaceType, serviceType, helmReleaseType}, got)
}

func TestStartSyncProcessHelmReleaseErrorIsolation(t *testing.T) {
	setupFixedTime(t)

	s := newHTTPSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/namespaces" {
			writeJSON(t, w, corev1.NamespaceList{Items: []corev1.Namespace{*newNamespace("team-a", nil)}})
			return
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	results := make(chan source.Data, 10)

	err := s.StartSyncProcess(t.Context(), map[string]source.Extra{namespaceType: {}, helmReleaseType: {}}, results)
	require.ErrorIs(t, err, ErrRetrievingAssets)
	close(results)

	items := collectData(results)
	require.Len(t, items, 1)
	assert.Equal(t, namespaceType, items[0].Type)
}

func newPod(namespace, name string, mutate ...func(*corev1.Pod)) *corev1.Pod {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name}}
	for _, fn := range mutate {
		fn(pod)
	}
	return pod
}

func ownedBy(kind, name string, controller *bool) func(*corev1.Pod) {
	return func(pod *corev1.Pod) {
		pod.OwnerReferences = append(pod.OwnerReferences, metav1.OwnerReference{Kind: kind, Name: name, Controller: controller})
	}
}

func withPhase(phase corev1.PodPhase) func(*corev1.Pod) {
	return func(pod *corev1.Pod) { pod.Status.Phase = phase }
}

func withLabels(pairs ...string) func(*corev1.Pod) {
	return func(pod *corev1.Pod) { pod.Labels = lbl(pairs...) }
}

func boolPtr(v bool) *bool { return &v }

func int64Ptr(v int64) *int64 { return &v }

func stringPtr(v string) *string { return &v }

func syncOnePod(t *testing.T, pod *corev1.Pod) map[string]any {
	t.Helper()
	items, err := syncOne(t, (*Source).syncPods, newFakeSource(t, pod))
	require.NoError(t, err)
	require.Len(t, items, 1)
	return items[0].Values
}

func TestSyncPodsEnvelope(t *testing.T) {
	setupFixedTime(t)

	s := newFakeSource(t, newPod("team-a", "web-0"), newPod("team-b", "web-0"))
	items, err := syncOne(t, (*Source).syncPods, s)
	require.NoError(t, err)
	require.Len(t, items, 2)
	for _, item := range items {
		assert.Equal(t, podType, item.Type)
		assert.Equal(t, source.DataOperationUpsert, item.Operation)
		assert.Equal(t, testFixedTime, item.Time)
		assert.Equal(t, testAPIServer, item.Values["apiServer"])
	}
}

func TestPodValuesEmptyPod(t *testing.T) {
	t.Parallel()

	got := podValues(newPod("team-a", "bare"), testAPIServer)
	want := map[string]any{
		"apiServer":          testAPIServer,
		"name":               "bare",
		"namespace":          "team-a",
		"labels":             map[string]string{},
		"annotations":        map[string]string{},
		"creationTimestamp":  "",
		"owner":              map[string]any{},
		"nodeName":           "",
		"phase":              "",
		"podIP":              "",
		"startTime":          "",
		"reason":             "",
		"containers":         []map[string]any{},
		"serviceAccountName": "",
		"hostNetwork":        false,
		"hostPID":            false,
		"hostIPC":            false,
		"nodeSelector":       map[string]string{},
		"tolerations":        []map[string]any{},
		"securityContext":    map[string]any{},
		"imagePullSecrets":   []string{},
		"volumes":            []map[string]any{},
	}
	assert.Equal(t, want, got)
}

func TestPodValuesFull(t *testing.T) {
	t.Parallel()

	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.FixedZone("x", 3600))
	started := metav1.NewTime(time.Date(2026, 1, 2, 3, 4, 10, 0, time.UTC))
	pod := newPod("team-a", "web-abc-xyz",
		withLabels("app", "web", "pod-template-hash", "abc"),
		ownedBy("ReplicaSet", "web-abc", boolPtr(true)),
		func(p *corev1.Pod) {
			p.Annotations = map[string]string{"note": "Hello, World", "empty": ""}
			p.CreationTimestamp = metav1.NewTime(created)
			p.Spec.NodeName = "node-1"
			p.Spec.ServiceAccountName = "web-sa"
			p.Spec.HostNetwork = true
			p.Spec.HostPID = true
			p.Spec.HostIPC = true
			p.Spec.NodeSelector = map[string]string{"disk": "ssd"}
			p.Spec.ImagePullSecrets = []corev1.LocalObjectReference{{Name: "regcred"}, {Name: "other"}}
			p.Status.Phase = corev1.PodFailed
			p.Status.PodIP = "10.0.0.7"
			p.Status.StartTime = &started
			p.Status.Reason = "Evicted"
		},
	)

	got := podValues(pod, testAPIServer)
	assert.Equal(t, map[string]string{"note": "Hello, World", "empty": ""}, got["annotations"])
	assert.Equal(t, map[string]string{"app": "web", "pod-template-hash": "abc"}, got["labels"])
	assert.Equal(t, "2026-01-02T02:04:05Z", got["creationTimestamp"])
	assert.Equal(t, "2026-01-02T03:04:10Z", got["startTime"])
	assert.Equal(t, map[string]any{"kind": "Deployment", "name": "web"}, got["owner"])
	assert.Equal(t, "node-1", got["nodeName"])
	assert.Equal(t, "Failed", got["phase"])
	assert.Equal(t, "10.0.0.7", got["podIP"])
	assert.Equal(t, "Evicted", got["reason"])
	assert.Equal(t, "web-sa", got["serviceAccountName"])
	assert.Equal(t, true, got["hostNetwork"])
	assert.Equal(t, true, got["hostPID"])
	assert.Equal(t, true, got["hostIPC"])
	assert.Equal(t, map[string]string{"disk": "ssd"}, got["nodeSelector"])
	assert.Equal(t, []string{"regcred", "other"}, got["imagePullSecrets"])
}

func TestPodValuesDoNotAliasInputs(t *testing.T) {
	t.Parallel()

	pod := newPod("team-a", "web", withLabels("a", "b"))
	pod.Annotations = map[string]string{"k": "v"}
	got := podValues(pod, testAPIServer)
	labels, ok := got["labels"].(map[string]string)
	require.True(t, ok)
	labels["x"] = "y"
	assert.Equal(t, map[string]string{"a": "b"}, pod.Labels)
}

func TestPodOwnerDerivation(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		pod  *corev1.Pod
		want map[string]any
	}{
		"replicaset of a deployment": {
			pod:  newPod("n", "p", withLabels("pod-template-hash", "5d8f7"), ownedBy("ReplicaSet", "my-app-5d8f7", boolPtr(true))),
			want: map[string]any{"kind": "Deployment", "name": "my-app"},
		},
		"replicaset without the hash label": {
			pod:  newPod("n", "p", ownedBy("ReplicaSet", "my-app-5d8f7", boolPtr(true))),
			want: map[string]any{"kind": "ReplicaSet", "name": "my-app-5d8f7"},
		},
		"replicaset whose name does not end with the hash": {
			pod:  newPod("n", "p", withLabels("pod-template-hash", "zzzz"), ownedBy("ReplicaSet", "my-app-5d8f7", nil)),
			want: map[string]any{"kind": "ReplicaSet", "name": "my-app-5d8f7"},
		},
		"replicaset named exactly like the suffix": {
			pod:  newPod("n", "p", withLabels("pod-template-hash", "abc"), ownedBy("ReplicaSet", "-abc", nil)),
			want: map[string]any{"kind": "ReplicaSet", "name": "-abc"},
		},
		"statefulset": {
			pod:  newPod("n", "p", ownedBy("StatefulSet", "db", boolPtr(true))),
			want: map[string]any{"kind": "StatefulSet", "name": "db"},
		},
		"daemonset": {
			pod:  newPod("n", "p", ownedBy("DaemonSet", "agent", nil)),
			want: map[string]any{"kind": "DaemonSet", "name": "agent"},
		},
		"job": {
			pod:  newPod("n", "p", ownedBy("Job", "batch-1", boolPtr(true))),
			want: map[string]any{"kind": "Job", "name": "batch-1"},
		},
		"no owner": {
			pod:  newPod("n", "p"),
			want: map[string]any{},
		},
		"controller preferred over first": {
			pod: newPod("n", "p",
				ownedBy("Foo", "first", nil),
				ownedBy("StatefulSet", "db", boolPtr(true)),
				ownedBy("Bar", "third", boolPtr(false)),
			),
			want: map[string]any{"kind": "StatefulSet", "name": "db"},
		},
		"first when no controller": {
			pod:  newPod("n", "p", ownedBy("Foo", "first", boolPtr(false)), ownedBy("Bar", "second", nil)),
			want: map[string]any{"kind": "Foo", "name": "first"},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, test.want, podValues(test.pod, testAPIServer)["owner"])
		})
	}
}

func TestSyncPodsSkipsSucceeded(t *testing.T) {
	setupFixedTime(t)

	s := newFakeSource(t,
		newPod("n", "done", withPhase(corev1.PodSucceeded)),
		newPod("n", "failed", withPhase(corev1.PodFailed)),
		newPod("n", "pending", withPhase(corev1.PodPending)),
		newPod("n", "running", withPhase(corev1.PodRunning)),
		newPod("n", "unknown", withPhase(corev1.PodUnknown)),
		newPod("n", "nophase"),
	)
	items, err := syncOne(t, (*Source).syncPods, s)
	require.NoError(t, err)

	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Values["name"].(string)) //nolint:forcetypeassert // always a string
	}
	assert.ElementsMatch(t, []string{"failed", "pending", "running", "unknown", "nophase"}, names)
}

func TestPodContainers(t *testing.T) {
	t.Parallel()

	pod := newPod("n", "p", func(p *corev1.Pod) {
		p.Spec.InitContainers = []corev1.Container{{Name: "init", Image: "init:1"}}
		p.Spec.Containers = []corev1.Container{
			{
				Name:  "app",
				Image: "app:1",
				Env:   []corev1.EnvVar{{Name: "SECRET", Value: "do-not-leak"}},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m")},
					Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")},
				},
			},
			{Name: "sidecar", Image: "side:2"},
		}
		p.Status.ContainerStatuses = []corev1.ContainerStatus{
			{Name: "other", ImageID: "docker://other"},
			{Name: "app", ImageID: "docker://sha256:abc", Ready: true, RestartCount: 3},
		}
	})

	got := podValues(pod, testAPIServer)["containers"]
	want := []map[string]any{
		{
			"name": "app", "image": "app:1", "imageID": "docker://sha256:abc",
			"resources":       map[string]any{"requests": map[string]string{"cpu": "500m"}, "limits": map[string]string{"memory": "1Gi"}},
			"securityContext": map[string]any{},
		},
		{
			"name": "sidecar", "image": "side:2", "imageID": "",
			"resources":       emptyResources(),
			"securityContext": map[string]any{},
		},
	}
	assert.Equal(t, want, got)
}

func TestPodSecurityContexts(t *testing.T) {
	t.Parallel()

	t.Run("container: every key", func(t *testing.T) {
		t.Parallel()
		sc := &corev1.SecurityContext{
			Privileged:               boolPtr(false),
			RunAsNonRoot:             boolPtr(true),
			RunAsUser:                int64Ptr(1000),
			RunAsGroup:               int64Ptr(0),
			ReadOnlyRootFilesystem:   boolPtr(true),
			AllowPrivilegeEscalation: boolPtr(false),
			Capabilities:             &corev1.Capabilities{Add: []corev1.Capability{"NET_ADMIN"}, Drop: []corev1.Capability{"ALL"}},
			SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeLocalhost, LocalhostProfile: stringPtr("profiles/a.json")},
		}
		assert.Equal(t, map[string]any{
			"privileged":               false,
			"runAsNonRoot":             true,
			"runAsUser":                int64(1000),
			"runAsGroup":               int64(0),
			"readOnlyRootFilesystem":   true,
			"allowPrivilegeEscalation": false,
			"capabilities":             map[string]any{"add": []string{"NET_ADMIN"}, "drop": []string{"ALL"}},
			"seccompProfile":           map[string]any{"type": "Localhost", "localhostProfile": "profiles/a.json"},
		}, containerSecurityContextValues(sc))
	})

	t.Run("container: only set keys", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, map[string]any{"privileged": true}, containerSecurityContextValues(&corev1.SecurityContext{Privileged: boolPtr(true)}))
		assert.Equal(t, map[string]any{}, containerSecurityContextValues(&corev1.SecurityContext{}))
		assert.Equal(t, map[string]any{}, containerSecurityContextValues(nil))
		assert.Equal(t,
			map[string]any{"capabilities": map[string]any{"add": []string{}, "drop": []string{"ALL"}}},
			containerSecurityContextValues(&corev1.SecurityContext{Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}}),
		)
		assert.Equal(t,
			map[string]any{"seccompProfile": map[string]any{"type": "RuntimeDefault"}},
			containerSecurityContextValues(&corev1.SecurityContext{SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault, LocalhostProfile: stringPtr("")}}),
		)
	})

	t.Run("container in the pod values", func(t *testing.T) {
		t.Parallel()
		pod := newPod("n", "p", func(p *corev1.Pod) {
			p.Spec.Containers = []corev1.Container{{Name: "c", SecurityContext: &corev1.SecurityContext{RunAsUser: int64Ptr(5)}}}
		})
		containers, ok := podValues(pod, testAPIServer)["containers"].([]map[string]any)
		require.True(t, ok)
		assert.Equal(t, map[string]any{"runAsUser": int64(5)}, containers[0]["securityContext"])
	})

	t.Run("pod: every key", func(t *testing.T) {
		t.Parallel()
		sc := &corev1.PodSecurityContext{
			RunAsUser:          int64Ptr(1),
			RunAsGroup:         int64Ptr(2),
			RunAsNonRoot:       boolPtr(true),
			FSGroup:            int64Ptr(3),
			SupplementalGroups: []int64{4, 5},
			SeccompProfile:     &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
			SELinuxOptions:     &corev1.SELinuxOptions{User: "ignored"},
		}
		assert.Equal(t, map[string]any{
			"runAsUser":          int64(1),
			"runAsGroup":         int64(2),
			"runAsNonRoot":       true,
			"fsGroup":            int64(3),
			"supplementalGroups": []int64{4, 5},
			"seccompProfile":     map[string]any{"type": "RuntimeDefault"},
		}, podSecurityContextValues(sc))
	})

	t.Run("pod: only set keys", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, map[string]any{}, podSecurityContextValues(nil))
		assert.Equal(t, map[string]any{}, podSecurityContextValues(&corev1.PodSecurityContext{}))
		assert.Equal(t, map[string]any{"fsGroup": int64(9)}, podSecurityContextValues(&corev1.PodSecurityContext{FSGroup: int64Ptr(9)}))
	})
}

func TestPodTolerations(t *testing.T) {
	t.Parallel()

	got := tolerationValues([]corev1.Toleration{
		{Key: "dedicated", Operator: corev1.TolerationOpEqual, Value: "gpu", Effect: corev1.TaintEffectNoSchedule},
		{Operator: corev1.TolerationOpExists},
		{Key: "node.kubernetes.io/not-ready", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoExecute, TolerationSeconds: int64Ptr(300)},
		{},
	})
	assert.Equal(t, []map[string]any{
		{"key": "dedicated", "operator": "Equal", "value": "gpu", "effect": "NoSchedule"},
		{"operator": "Exists"},
		{"key": "node.kubernetes.io/not-ready", "operator": "Exists", "effect": "NoExecute", "tolerationSeconds": int64(300)},
		{},
	}, got)
	assert.Equal(t, []map[string]any{}, tolerationValues(nil))
}

func TestPodVolumes(t *testing.T) {
	t.Parallel()

	volumes := []corev1.Volume{
		{Name: "sec", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "my-secret", Items: []corev1.KeyToPath{{Key: "password", Path: "p"}}}}},
		{Name: "cfg", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: "my-config"}}}},
		{Name: "data", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "my-claim"}}},
		{Name: "tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		{Name: "host", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/var/log"}}},
		{Name: "proj", VolumeSource: corev1.VolumeSource{Projected: &corev1.ProjectedVolumeSource{Sources: []corev1.VolumeProjection{{
			Secret: &corev1.SecretProjection{LocalObjectReference: corev1.LocalObjectReference{Name: "hidden"}},
		}}}}},
		{Name: "down", VolumeSource: corev1.VolumeSource{DownwardAPI: &corev1.DownwardAPIVolumeSource{}}},
		{Name: "csi", VolumeSource: corev1.VolumeSource{CSI: &corev1.CSIVolumeSource{Driver: "csi.example.com", VolumeAttributes: map[string]string{"token": "do-not-leak"}}}},
		{Name: "share", VolumeSource: corev1.VolumeSource{NFS: &corev1.NFSVolumeSource{Server: "nfs.example.com", Path: "/export"}}},
		{Name: "none"},
	}

	got := volumeValues(volumes)
	assert.Equal(t, []map[string]any{
		{"name": "sec", "type": "secret", "ref": "my-secret"},
		{"name": "cfg", "type": "configMap", "ref": "my-config"},
		{"name": "data", "type": "persistentVolumeClaim", "ref": "my-claim"},
		{"name": "tmp", "type": "emptyDir", "ref": ""},
		{"name": "host", "type": "hostPath", "ref": "/var/log"},
		{"name": "proj", "type": "projected", "ref": ""},
		{"name": "down", "type": "downwardAPI", "ref": ""},
		{"name": "csi", "type": "csi", "ref": "csi.example.com"},
		{"name": "share", "type": "nfs", "ref": ""},
		{"name": "none", "type": "unknown", "ref": ""},
	}, got)

	encoded, err := json.Marshal(got)
	require.NoError(t, err)
	for _, leaked := range []string{"password", "hidden", "do-not-leak", "nfs.example.com", "/export"} {
		assert.NotContains(t, string(encoded), leaked)
	}
	assert.Equal(t, []map[string]any{}, volumeValues(nil))
}

func TestSyncPodsValuesThroughFakeClientset(t *testing.T) {
	setupFixedTime(t)

	values := syncOnePod(t, newPod("team-a", "web", ownedBy("DaemonSet", "agent", nil)))
	assert.Equal(t, map[string]any{"kind": "DaemonSet", "name": "agent"}, values["owner"])
}

func TestSyncPodsEmpty(t *testing.T) {
	t.Parallel()

	items, err := syncOne(t, (*Source).syncPods, newFakeSource(t))
	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestSyncPodsPagination(t *testing.T) {
	setupFixedTime(t)

	var limits []string
	s := newHTTPSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/pods", r.URL.Path)
		limits = append(limits, r.URL.Query().Get("limit"))
		if r.URL.Query().Get("continue") == "" {
			writeJSON(t, w, corev1.PodList{
				ListMeta: metav1.ListMeta{Continue: "page-2"},
				Items:    []corev1.Pod{*newPod("team-a", "one")},
			})
			return
		}
		assert.Equal(t, "page-2", r.URL.Query().Get("continue"))
		writeJSON(t, w, corev1.PodList{Items: []corev1.Pod{*newPod("team-b", "two"), *newPod("team-b", "done", withPhase(corev1.PodSucceeded))}})
	}))

	items, err := syncOne(t, (*Source).syncPods, s)
	require.NoError(t, err)
	assert.Len(t, items, 2)
	assert.Equal(t, []string{"500", "500"}, limits)
}

func TestSyncPodsListError(t *testing.T) {
	t.Parallel()

	s := newHTTPSource(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	require.ErrorIs(t, s.syncPods(t.Context(), make(chan source.Data, 1)), ErrRetrievingAssets)
}

func TestSyncPodsContextCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	s := newFakeSource(t, newPod("team-a", "web"))
	require.ErrorIs(t, s.syncPods(ctx, make(chan source.Data)), context.Canceled)
}

func TestSyncPodsCanceledWhileSending(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	s := newFakeSource(t, newPod("team-a", "web"))

	done := make(chan error, 1)
	go func() { done <- s.syncPods(ctx, make(chan source.Data)) }()
	cancel()

	require.ErrorIs(t, <-done, context.Canceled)
}

func TestStartSyncProcessPodDispatchOrder(t *testing.T) {
	setupFixedTime(t)

	s := newFakeSource(t,
		newNamespace("team-a", nil),
		newService("team-a", "web", nil, corev1.ServiceSpec{}),
		newPod("team-a", "web-0", ownedBy("StatefulSet", "web", boolPtr(true))),
	)
	results := make(chan source.Data, 10)

	types := map[string]source.Extra{podOwnerRelationshipType: {}, podType: {}, serviceType: {}, namespaceType: {}, "unknown": {}}
	require.NoError(t, s.StartSyncProcess(t.Context(), types, results))
	close(results)

	items := collectData(results)
	gotTypes := make([]string, 0, len(items))
	for _, item := range items {
		gotTypes = append(gotTypes, item.Type)
	}
	assert.Equal(t, []string{namespaceType, serviceType, podType, podOwnerRelationshipType}, gotTypes)
}

func TestStartSyncProcessPodErrorIsolation(t *testing.T) {
	setupFixedTime(t)

	// pods are forbidden, namespaces are still served.
	s := newHTTPSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/namespaces" {
			writeJSON(t, w, corev1.NamespaceList{Items: []corev1.Namespace{*newNamespace("team-a", nil)}})
			return
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	results := make(chan source.Data, 10)

	err := s.StartSyncProcess(t.Context(), map[string]source.Extra{namespaceType: {}, podType: {}}, results)
	require.ErrorIs(t, err, ErrK8sSource)
	require.ErrorIs(t, err, ErrRetrievingAssets)
	close(results)

	items := collectData(results)
	require.Len(t, items, 1)
	assert.Equal(t, namespaceType, items[0].Type)
}

func TestPodTypesAreKnownAndStreamable(t *testing.T) {
	t.Parallel()

	for _, name := range []string{podType, podOwnerRelationshipType} {
		assert.True(t, isKnownType(name), name)
		assert.True(t, isStreamable(name), name)
	}
	assert.Equal(t, networkPolicyType, knownTypes[9].name)
	assert.Equal(t, podType, knownTypes[10].name)
	assert.Equal(t, podOwnerRelationshipType, knownTypes[len(knownTypes)-1].name)
}
