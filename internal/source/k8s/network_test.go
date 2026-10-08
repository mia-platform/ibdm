// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package k8s

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/mia-platform/ibdm/internal/source"
)

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
