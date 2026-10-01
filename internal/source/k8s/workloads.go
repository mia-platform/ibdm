// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package k8s

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/mia-platform/ibdm/internal/source"
)

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
			values := s.workloadValues(&deployment.ObjectMeta, &deployment.Spec.Template.Spec)
			values["replicas"] = replicasOrDefault(deployment.Spec.Replicas)
			return send(ctx, results, s.workloadData(deploymentType, values))
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
			values := s.workloadValues(&statefulSet.ObjectMeta, &statefulSet.Spec.Template.Spec)
			values["replicas"] = replicasOrDefault(statefulSet.Spec.Replicas)
			return send(ctx, results, s.workloadData(statefulSetType, values))
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
			values := s.workloadValues(&daemonSet.ObjectMeta, &daemonSet.Spec.Template.Spec)
			return send(ctx, results, s.workloadData(daemonSetType, values))
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

// workloadValues builds the values shared by every workload kind. Labels and
// container lists are never nil so that they render as {} and [] in templates.
func (s *Source) workloadValues(meta *metav1.ObjectMeta, pod *corev1.PodSpec) map[string]any {
	labels := make(map[string]string, len(meta.Labels))
	for key, value := range meta.Labels {
		labels[key] = value
	}

	return map[string]any{
		keyAPIServer:     s.apiServer,
		keyName:          meta.Name,
		"namespace":      meta.Namespace,
		"labels":         labels,
		"containers":     containerInfos(pod.Containers),
		"initContainers": containerInfos(pod.InitContainers),
	}
}

// containerInfos converts containers into {name, image} maps, preserving the
// spec order. The returned slice is never nil.
func containerInfos(containers []corev1.Container) []map[string]any {
	infos := make([]map[string]any, 0, len(containers))
	for _, container := range containers {
		infos = append(infos, map[string]any{
			"name":  container.Name,
			"image": container.Image,
		})
	}
	return infos
}

// replicasOrDefault returns the desired replicas, or the Kubernetes default when unset.
func replicasOrDefault(replicas *int32) int {
	if replicas == nil {
		return defaultReplicas
	}
	return int(*replicas)
}
