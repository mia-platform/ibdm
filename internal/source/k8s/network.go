// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package k8s

import (
	"context"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/mia-platform/ibdm/internal/source"
)

const (
	keyPodSelector       = "podSelector"
	keyNamespaceSelector = "namespaceSelector"
	keyMatchLabels       = "matchLabels"
	keyMatchExpressions  = "matchExpressions"
	keyOperator          = "operator"
	keyValues            = "values"
	keyIPBlock           = "ipBlock"
	keyCIDR              = "cidr"
	keyExcept            = "except"
	keyProtocol          = "protocol"
	keyEndPort           = "endPort"
	keyPolicyTypes       = "policyTypes"
	keyIngress           = "ingress"
	keyEgress            = "egress"
	keyFrom              = "from"
	keyTo                = "to"
	keyPorts             = "ports"
	keyKey               = "key"
)

// syncNetworkPolicies emits one networkpolicy item per NetworkPolicy of the cluster.
func (s *Source) syncNetworkPolicies(ctx context.Context, results chan<- source.Data) error {
	return listPages(ctx, "networkpolicies",
		func(ctx context.Context, options metav1.ListOptions) ([]networkingv1.NetworkPolicy, string, error) {
			list, err := s.clientset.NetworkingV1().NetworkPolicies(metav1.NamespaceAll).List(ctx, options)
			if err != nil {
				return nil, "", err
			}
			return list.Items, list.Continue, nil
		},
		func(policy *networkingv1.NetworkPolicy) error {
			return send(ctx, results, s.workloadData(networkPolicyType, networkPolicyValues(policy, s.apiServer)))
		},
	)
}

// networkPolicyValues builds the values of a networkpolicy item. Maps and lists
// are never nil so that they render as {} and [] in templates. An empty
// ingress/egress list (deny all) is kept distinct from a rule without peers and
// ports (allow all).
func networkPolicyValues(policy *networkingv1.NetworkPolicy, apiServer string) map[string]any {
	spec := &policy.Spec
	return map[string]any{
		keyAPIServer:   apiServer,
		keyName:        policy.Name,
		keyNamespace:   policy.Namespace,
		keyLabels:      copyStringMap(policy.Labels),
		keyPodSelector: labelSelectorValues(&spec.PodSelector),
		keyPolicyTypes: effectivePolicyTypes(spec),
		keyIngress:     ingressRuleValues(spec.Ingress),
		keyEgress:      egressRuleValues(spec.Egress),
	}
}

// effectivePolicyTypes returns the policy types of spec as Kubernetes applies
// them: the declared ones, or Ingress plus Egress when the policy has egress rules.
func effectivePolicyTypes(spec *networkingv1.NetworkPolicySpec) []string {
	types := make([]string, 0, 2)
	if len(spec.PolicyTypes) > 0 {
		for _, policyType := range spec.PolicyTypes {
			types = append(types, string(policyType))
		}
		return types
	}

	types = append(types, string(networkingv1.PolicyTypeIngress))
	if len(spec.Egress) > 0 {
		types = append(types, string(networkingv1.PolicyTypeEgress))
	}
	return types
}

// labelSelectorValues converts a label selector. An empty selector keeps both
// keys, empty, because it selects everything.
func labelSelectorValues(selector *metav1.LabelSelector) map[string]any {
	expressions := make([]map[string]any, 0, len(selector.MatchExpressions))
	for _, expression := range selector.MatchExpressions {
		expressions = append(expressions, map[string]any{
			keyKey:      expression.Key,
			keyOperator: string(expression.Operator),
			keyValues:   append([]string{}, expression.Values...),
		})
	}

	return map[string]any{
		keyMatchLabels:      copyStringMap(selector.MatchLabels),
		keyMatchExpressions: expressions,
	}
}

func ingressRuleValues(rules []networkingv1.NetworkPolicyIngressRule) []map[string]any {
	out := make([]map[string]any, 0, len(rules))
	for _, rule := range rules {
		out = append(out, map[string]any{
			keyFrom:  peerValues(rule.From),
			keyPorts: policyPortValues(rule.Ports),
		})
	}
	return out
}

func egressRuleValues(rules []networkingv1.NetworkPolicyEgressRule) []map[string]any {
	out := make([]map[string]any, 0, len(rules))
	for _, rule := range rules {
		out = append(out, map[string]any{
			keyTo:    peerValues(rule.To),
			keyPorts: policyPortValues(rule.Ports),
		})
	}
	return out
}

// peerValues converts policy peers, keeping only the keys that are set.
func peerValues(peers []networkingv1.NetworkPolicyPeer) []map[string]any {
	out := make([]map[string]any, 0, len(peers))
	for _, peer := range peers {
		values := map[string]any{}
		if peer.PodSelector != nil {
			values[keyPodSelector] = labelSelectorValues(peer.PodSelector)
		}
		if peer.NamespaceSelector != nil {
			values[keyNamespaceSelector] = labelSelectorValues(peer.NamespaceSelector)
		}
		if peer.IPBlock != nil {
			values[keyIPBlock] = map[string]any{
				keyCIDR:   peer.IPBlock.CIDR,
				keyExcept: append([]string{}, peer.IPBlock.Except...),
			}
		}
		out = append(out, values)
	}
	return out
}

// policyPortValues converts policy ports; the protocol defaults to TCP and unset
// port and endPort are omitted.
func policyPortValues(ports []networkingv1.NetworkPolicyPort) []map[string]any {
	out := make([]map[string]any, 0, len(ports))
	for _, port := range ports {
		protocol := "TCP"
		if port.Protocol != nil && *port.Protocol != "" {
			protocol = string(*port.Protocol)
		}

		values := map[string]any{keyProtocol: protocol}
		if port.Port != nil {
			if port.Port.Type == intstr.String {
				values[keyPort] = port.Port.StrVal
			} else {
				values[keyPort] = int(port.Port.IntVal)
			}
		}
		if port.EndPort != nil {
			values[keyEndPort] = int(*port.EndPort)
		}
		out = append(out, values)
	}
	return out
}
