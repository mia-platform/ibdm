// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package k8s

import (
	"context"
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
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
