// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package k8s

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/mia-platform/ibdm/internal/source"
)

// millisPerCore is the number of milli-CPUs that make up one core.
const millisPerCore = 1000

// syncCluster emits the single cluster item describing the monitored cluster.
// A Kubernetes cluster is not an API object, so its identity comes from the
// configured API server, while its node list, node count and total CPU cores
// are derived from the node list.
func (s *Source) syncCluster(ctx context.Context, results chan<- source.Data) error {
	nodes, err := s.listAllNodes(ctx)
	if err != nil {
		return err
	}

	return send(ctx, results, source.Data{
		Type:      clusterType,
		Operation: source.DataOperationUpsert,
		Values:    clusterValues(s.apiServer, s.clusterName, nodes),
		Time:      timeSource(),
	})
}

// clusterValues builds the values of the cluster item from the node list. It is
// shared by the sync and the watch mode.
func clusterValues(apiServer, clusterName string, nodes []corev1.Node) map[string]any {
	nodeInfos, totalCPUCores := buildNodeInfos(nodes)

	return map[string]any{
		keyAPIServer:    apiServer,
		"clusterName":   clusterName,
		"nodeCount":     len(nodeInfos),
		"nodes":         nodeInfos,
		"totalCPUCores": totalCPUCores,
	}
}

// buildNodeInfos converts the nodes into plain maps sorted by name, and returns
// them together with the sum of their CPU capacity expressed in cores.
// The returned slice is never nil, and every taints value is a non-nil slice.
func buildNodeInfos(nodes []corev1.Node) ([]map[string]any, float64) {
	infos := make([]map[string]any, 0, len(nodes))
	var totalCPUCores float64

	for i := range nodes {
		node := &nodes[i]

		cpuCores := 0.0
		if quantity, ok := node.Status.Capacity[corev1.ResourceCPU]; ok {
			cpuCores = float64(quantity.MilliValue()) / millisPerCore
		}
		totalCPUCores += cpuCores

		taints := make([]map[string]any, 0, len(node.Spec.Taints))
		for _, taint := range node.Spec.Taints {
			taints = append(taints, map[string]any{
				"key":    taint.Key,
				"value":  taint.Value,
				"effect": string(taint.Effect),
			})
		}

		infos = append(infos, map[string]any{
			keyName:          node.Name,
			"kubeletVersion": node.Status.NodeInfo.KubeletVersion,
			"osImage":        node.Status.NodeInfo.OSImage,
			"kernelVersion":  node.Status.NodeInfo.KernelVersion,
			"architecture":   node.Status.NodeInfo.Architecture,
			"cpuCapacity":    cpuCores,
			"taints":         taints,
		})
	}

	sort.SliceStable(infos, func(i, j int) bool {
		return infos[i]["name"].(string) < infos[j]["name"].(string) //nolint:forcetypeassert // always a string
	})
	return infos, totalCPUCores
}

// syncNamespaces emits one namespace item per namespace of the cluster,
// following the pagination continue token until the last page.
func (s *Source) syncNamespaces(ctx context.Context, results chan<- source.Data) error {
	log := logger.FromContext(ctx).WithName(loggerName)
	options := metav1.ListOptions{Limit: pageSize}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		list, err := s.clientset.CoreV1().Namespaces().List(ctx, options)
		if err != nil {
			return fmt.Errorf("%w: listing namespaces: %w", ErrRetrievingAssets, err)
		}

		for i := range list.Items {
			values, err := namespaceValues(&list.Items[i], s.apiServer)
			if err != nil {
				log.Error("error converting namespace, skipping", "namespace", list.Items[i].Name, "error", err.Error())
				continue
			}

			if err := send(ctx, results, s.workloadData(namespaceType, values)); err != nil {
				return err
			}
		}

		if list.Continue == "" {
			return nil
		}
		options.Continue = list.Continue
	}
}

// namespaceValues builds the values of a namespace item. The namespace is
// converted into an unstructured map without its managed fields.
func namespaceValues(namespace *corev1.Namespace, apiServer string) (map[string]any, error) {
	converted, err := runtime.DefaultUnstructuredConverter.ToUnstructured(namespace)
	if err != nil {
		return nil, fmt.Errorf("converting namespace: %w", err)
	}
	if metadata, ok := converted["metadata"].(map[string]any); ok {
		delete(metadata, "managedFields")
	}

	return map[string]any{
		keyNamespace: converted,
		keyAPIServer: apiServer,
	}, nil
}

// listAllNodes returns every node of the cluster, following the pagination
// continue token until the API server reports no more pages.
func (s *Source) listAllNodes(ctx context.Context) ([]corev1.Node, error) {
	var nodes []corev1.Node
	options := metav1.ListOptions{Limit: pageSize}

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		list, err := s.clientset.CoreV1().Nodes().List(ctx, options)
		if err != nil {
			return nil, fmt.Errorf("%w: listing nodes: %w", ErrRetrievingAssets, err)
		}

		nodes = append(nodes, list.Items...)
		if list.Continue == "" {
			return nodes, nil
		}
		options.Continue = list.Continue
	}
}

// Value keys used only by service items.
const (
	keyType     = "type"
	keyPort     = "port"
	keyIP       = "ip"
	keyHostname = "hostname"
)

// syncServices emits one service item per service of the cluster.
func (s *Source) syncServices(ctx context.Context, results chan<- source.Data) error {
	return listPages(ctx, "services",
		func(ctx context.Context, options metav1.ListOptions) ([]corev1.Service, string, error) {
			list, err := s.clientset.CoreV1().Services(metav1.NamespaceAll).List(ctx, options)
			if err != nil {
				return nil, "", err
			}
			return list.Items, list.Continue, nil
		},
		func(service *corev1.Service) error {
			return send(ctx, results, s.workloadData(serviceType, serviceValues(service, s.apiServer)))
		},
	)
}

// serviceValues builds the values of a service item. Maps and lists are never
// nil so that they render as {} and [] in templates.
func serviceValues(service *corev1.Service, apiServer string) map[string]any {
	kind := string(service.Spec.Type)
	if kind == "" {
		kind = string(corev1.ServiceTypeClusterIP)
	}

	return map[string]any{
		keyAPIServer:   apiServer,
		keyName:        service.Name,
		keyNamespace:   service.Namespace,
		keyLabels:      copyStringMap(service.Labels),
		keyType:        kind,
		"clusterIPs":   serviceClusterIPs(&service.Spec),
		"ports":        servicePorts(service.Spec.Ports),
		"loadBalancer": serviceLoadBalancer(service.Status.LoadBalancer.Ingress),
		"externalIPs":  append([]string{}, service.Spec.ExternalIPs...),
		"externalName": service.Spec.ExternalName,
		"selector":     copyStringMap(service.Spec.Selector),
	}
}

// copyStringMap returns a non-nil copy of in.
func copyStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

// serviceClusterIPs returns spec.clusterIPs, falling back to spec.clusterIP.
// "None" (headless) is kept as is. The returned slice is never nil.
func serviceClusterIPs(spec *corev1.ServiceSpec) []string {
	if len(spec.ClusterIPs) > 0 {
		return append([]string{}, spec.ClusterIPs...)
	}
	if spec.ClusterIP != "" {
		return []string{spec.ClusterIP}
	}
	return []string{}
}

// servicePorts converts service ports into plain maps, preserving the spec
// order. The returned slice is never nil.
func servicePorts(ports []corev1.ServicePort) []map[string]any {
	infos := make([]map[string]any, 0, len(ports))
	for _, port := range ports {
		protocol := string(port.Protocol)
		if protocol == "" {
			protocol = string(corev1.ProtocolTCP)
		}

		infos = append(infos, map[string]any{
			keyName:      port.Name,
			"protocol":   protocol,
			keyPort:      int(port.Port),
			"targetPort": targetPortValue(port),
			"nodePort":   int(port.NodePort),
		})
	}
	return infos
}

// targetPortValue returns the target port as a number or as a port name. When
// the target port is unset Kubernetes targets the service port itself.
func targetPortValue(port corev1.ServicePort) any {
	target := port.TargetPort
	switch {
	case target.Type == intstr.String && target.StrVal != "":
		return target.StrVal
	case target.Type == intstr.Int && target.IntVal != 0:
		return int(target.IntVal)
	default:
		return int(port.Port)
	}
}

// serviceLoadBalancer converts load balancer ingress points into {ip, hostname}
// maps. The returned slice is never nil.
func serviceLoadBalancer(ingress []corev1.LoadBalancerIngress) []map[string]any {
	infos := make([]map[string]any, 0, len(ingress))
	for _, entry := range ingress {
		infos = append(infos, map[string]any{keyIP: entry.IP, keyHostname: entry.Hostname})
	}
	return infos
}

// defaultReplicas is the replica count Kubernetes applies when spec.replicas is unset.
const defaultReplicas = 1

// Value keys shared by several data types.
const (
	keyAPIServer = "apiServer"
	keyName      = "name"
)

// syncDeployments emits one deployment item per deployment of the cluster.
func (s *Source) syncDeployments(ctx context.Context, results chan<- source.Data) error {
	return listPages(ctx, "deployments",
		func(ctx context.Context, options metav1.ListOptions) ([]appsv1.Deployment, string, error) {
			list, err := s.clientset.AppsV1().Deployments(metav1.NamespaceAll).List(ctx, options)
			if err != nil {
				return nil, "", err
			}
			return list.Items, list.Continue, nil
		},
		func(deployment *appsv1.Deployment) error {
			return send(ctx, results, s.workloadData(deploymentType, deploymentValues(deployment, s.apiServer)))
		},
	)
}

// syncStatefulSets emits one statefulset item per statefulset of the cluster.
func (s *Source) syncStatefulSets(ctx context.Context, results chan<- source.Data) error {
	return listPages(ctx, "statefulsets",
		func(ctx context.Context, options metav1.ListOptions) ([]appsv1.StatefulSet, string, error) {
			list, err := s.clientset.AppsV1().StatefulSets(metav1.NamespaceAll).List(ctx, options)
			if err != nil {
				return nil, "", err
			}
			return list.Items, list.Continue, nil
		},
		func(statefulSet *appsv1.StatefulSet) error {
			return send(ctx, results, s.workloadData(statefulSetType, statefulSetValues(statefulSet, s.apiServer)))
		},
	)
}

// syncDaemonSets emits one daemonset item per daemonset of the cluster.
// DaemonSets have no replicas field, so none is emitted.
func (s *Source) syncDaemonSets(ctx context.Context, results chan<- source.Data) error {
	return listPages(ctx, "daemonsets",
		func(ctx context.Context, options metav1.ListOptions) ([]appsv1.DaemonSet, string, error) {
			list, err := s.clientset.AppsV1().DaemonSets(metav1.NamespaceAll).List(ctx, options)
			if err != nil {
				return nil, "", err
			}
			return list.Items, list.Continue, nil
		},
		func(daemonSet *appsv1.DaemonSet) error {
			return send(ctx, results, s.workloadData(daemonSetType, daemonSetValues(daemonSet, s.apiServer)))
		},
	)
}

// listPages calls list until the API server reports no more pages, invoking
// handle for every returned item. The resource name is only used in errors.
func listPages[T any](
	ctx context.Context,
	resource string,
	list func(ctx context.Context, options metav1.ListOptions) ([]T, string, error),
	handle func(item *T) error,
) error {
	options := metav1.ListOptions{Limit: pageSize}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		items, next, err := list(ctx, options)
		if err != nil {
			return fmt.Errorf("%w: listing %s: %w", ErrRetrievingAssets, resource, err)
		}

		for i := range items {
			if err := handle(&items[i]); err != nil {
				return err
			}
		}

		if next == "" {
			return nil
		}
		options.Continue = next
	}
}

// workloadData wraps values into an upsert source.Data of the given type.
func (s *Source) workloadData(dataType string, values map[string]any) source.Data {
	return source.Data{
		Type:      dataType,
		Operation: source.DataOperationUpsert,
		Values:    values,
		Time:      timeSource(),
	}
}

// deploymentValues builds the values of a deployment item.
func deploymentValues(deployment *appsv1.Deployment, apiServer string) map[string]any {
	values := workloadValues(&deployment.ObjectMeta, &deployment.Spec.Template.Spec, apiServer)
	values["replicas"] = replicasOrDefault(deployment.Spec.Replicas)
	return values
}

// statefulSetValues builds the values of a statefulset item.
func statefulSetValues(statefulSet *appsv1.StatefulSet, apiServer string) map[string]any {
	values := workloadValues(&statefulSet.ObjectMeta, &statefulSet.Spec.Template.Spec, apiServer)
	values["replicas"] = replicasOrDefault(statefulSet.Spec.Replicas)
	return values
}

// daemonSetValues builds the values of a daemonset item. DaemonSets have no
// replicas field, so none is emitted.
func daemonSetValues(daemonSet *appsv1.DaemonSet, apiServer string) map[string]any {
	return workloadValues(&daemonSet.ObjectMeta, &daemonSet.Spec.Template.Spec, apiServer)
}

// workloadValues builds the values shared by every workload kind. Labels and
// container lists are never nil so that they render as {} and [] in templates.
func workloadValues(meta *metav1.ObjectMeta, pod *corev1.PodSpec, apiServer string) map[string]any {
	labels := make(map[string]string, len(meta.Labels))
	for key, value := range meta.Labels {
		labels[key] = value
	}

	return map[string]any{
		keyAPIServer:     apiServer,
		keyName:          meta.Name,
		keyNamespace:     meta.Namespace,
		"labels":         labels,
		"containers":     containerInfos(pod.Containers),
		"initContainers": initContainerInfos(pod.InitContainers),
	}
}

// containerInfos converts containers into {name, image, resources} maps,
// preserving the spec order. The returned slice is never nil.
func containerInfos(containers []corev1.Container) []map[string]any {
	infos := make([]map[string]any, 0, len(containers))
	for _, container := range containers {
		infos = append(infos, map[string]any{
			"name":      container.Name,
			"image":     container.Image,
			"resources": resourceInfo(&container.Resources),
		})
	}
	return infos
}

// initContainerInfos converts init containers into {name, image} maps,
// preserving the spec order. The returned slice is never nil.
func initContainerInfos(containers []corev1.Container) []map[string]any {
	infos := make([]map[string]any, 0, len(containers))
	for _, container := range containers {
		infos = append(infos, map[string]any{
			"name":  container.Name,
			"image": container.Image,
		})
	}
	return infos
}

// resourceInfo converts container resources into {requests, limits} maps of
// resource name to canonical quantity string. Both maps are never nil.
func resourceInfo(resources *corev1.ResourceRequirements) map[string]any {
	return map[string]any{
		"requests": quantityStrings(resources.Requests),
		"limits":   quantityStrings(resources.Limits),
	}
}

// quantityStrings renders every quantity of the list with its canonical string form.
func quantityStrings(list corev1.ResourceList) map[string]string {
	result := make(map[string]string, len(list))
	for name, quantity := range list {
		result[string(name)] = quantity.String()
	}
	return result
}

// replicasOrDefault returns the desired replicas, or the Kubernetes default when unset.
func replicasOrDefault(replicas *int32) int {
	if replicas == nil {
		return defaultReplicas
	}
	return int(*replicas)
}

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

const (
	// helmSecretSelector selects the Secrets that store the current revision of every Helm release.
	helmSecretSelector = "owner=helm,status=deployed" //nolint:gosec // label selector, not a credential

	// helmReleaseKey is the Secret data key holding the encoded Helm release.
	helmReleaseKey = "release"

	// helmNameLabel and helmVersionLabel are the bookkeeping labels Helm puts on its release Secrets.
	helmNameLabel    = "name"
	helmVersionLabel = "version"

	// maxDecodedReleaseSize bounds the size of a decompressed release to protect against compression bombs.
	maxDecodedReleaseSize = 256 << 20
)

// gzipMagic is the prefix of every gzip stream.
var gzipMagic = []byte{0x1f, 0x8b}

// helmRelease is the minimal slice of Helm's release object that is decoded.
// The manifest, the values and the chart files are deliberately not captured.
type helmRelease struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Version   int    `json:"version"`
	Info      struct {
		Status        string     `json:"status"`
		FirstDeployed flexString `json:"first_deployed"` //nolint:tagliatelle // Helm storage format
		LastDeployed  flexString `json:"last_deployed"`  //nolint:tagliatelle // Helm storage format
	} `json:"info"`
	Chart struct {
		Metadata struct {
			Name       string `json:"name"`
			Version    string `json:"version"`
			AppVersion string `json:"appVersion"`
		} `json:"metadata"`
	} `json:"chart"`
}

// flexString is a string that tolerates any other JSON value, which it turns into an empty string.
type flexString string

// UnmarshalJSON keeps JSON strings and ignores every other JSON value.
func (f *flexString) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		*f = ""
		return nil //nolint:nilerr // non-string values are intentionally ignored
	}
	*f = flexString(value)
	return nil
}

// decodeHelmRelease decodes the content of a Helm release Secret: base64, then
// gzip when the gzip magic bytes are present, then JSON.
func decodeHelmRelease(data []byte) (*helmRelease, error) {
	raw := make([]byte, base64.StdEncoding.DecodedLen(len(data)))
	n, err := base64.StdEncoding.Decode(raw, data)
	if err != nil {
		return nil, fmt.Errorf("decoding base64: %w", err)
	}
	raw = raw[:n]

	if bytes.HasPrefix(raw, gzipMagic) {
		reader, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, fmt.Errorf("opening gzip stream: %w", err)
		}
		defer reader.Close()

		if raw, err = io.ReadAll(io.LimitReader(reader, maxDecodedReleaseSize)); err != nil {
			return nil, fmt.Errorf("reading gzip stream: %w", err)
		}
	}

	var release helmRelease
	if err := json.Unmarshal(raw, &release); err != nil {
		return nil, fmt.Errorf("decoding json: %w", err)
	}
	return &release, nil
}

// syncHelmReleases emits one helmrelease item per deployed Helm release of the
// cluster, read from Helm's release Secrets. Secrets that cannot be decoded are
// logged (name and namespace only) and skipped.
func (s *Source) syncHelmReleases(ctx context.Context, results chan<- source.Data) error {
	log := logger.FromContext(ctx).WithName(loggerName)
	releases := make(map[string]map[string]any)

	err := listPages(ctx, "helm release secrets",
		func(ctx context.Context, options metav1.ListOptions) ([]corev1.Secret, string, error) {
			options.LabelSelector = helmSecretSelector
			list, err := s.clientset.CoreV1().Secrets(metav1.NamespaceAll).List(ctx, options)
			if err != nil {
				return nil, "", err
			}
			return list.Items, list.Continue, nil
		},
		func(secret *corev1.Secret) error {
			values, err := helmSecretValues(secret, s.apiServer)
			if err != nil {
				log.Warn("error decoding helm release secret, skipping", "secret", secret.Name, "namespace", secret.Namespace, "error", err.Error())
				return nil
			}

			key := values[keyNamespace].(string) + "/" + values[keyName].(string)                          //nolint:forcetypeassert // always strings
			if current, ok := releases[key]; ok && current["revision"].(int) >= values["revision"].(int) { //nolint:forcetypeassert // always int
				return nil
			}
			releases[key] = values
			return nil
		},
	)
	if err != nil {
		return err
	}

	keys := make([]string, 0, len(releases))
	for key := range releases {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		if err := send(ctx, results, s.workloadData(helmReleaseType, releases[key])); err != nil {
			return err
		}
	}
	return nil
}

// helmSecretValues decodes a Helm release Secret and builds the values of the
// matching helmrelease item.
func helmSecretValues(secret *corev1.Secret, apiServer string) (map[string]any, error) {
	release, err := decodeHelmRelease(secret.Data[helmReleaseKey])
	if err != nil {
		return nil, err
	}
	return helmReleaseValues(secret, release, apiServer), nil
}

// helmReleaseValues builds the values of a helmrelease item. Name, namespace
// and revision fall back to the Secret's metadata when the release lacks them.
func helmReleaseValues(secret *corev1.Secret, release *helmRelease, apiServer string) map[string]any {
	name := release.Name
	if name == "" {
		name = secret.Labels[helmNameLabel]
	}
	namespace := release.Namespace
	if namespace == "" {
		namespace = secret.Namespace
	}
	revision := release.Version
	if revision == 0 {
		if parsed, err := strconv.Atoi(secret.Labels[helmVersionLabel]); err == nil {
			revision = parsed
		}
	}

	return map[string]any{
		keyAPIServer:    apiServer,
		keyName:         name,
		keyNamespace:    namespace,
		"revision":      revision,
		"status":        release.Info.Status,
		"chartName":     release.Chart.Metadata.Name,
		"chartVersion":  release.Chart.Metadata.Version,
		"appVersion":    release.Chart.Metadata.AppVersion,
		"firstDeployed": string(release.Info.FirstDeployed),
		"lastDeployed":  string(release.Info.LastDeployed),
	}
}
