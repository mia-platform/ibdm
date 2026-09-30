// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package k8s

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/mia-platform/ibdm/internal/source"
)

// syncCluster emits the single cluster item describing the monitored cluster.
// A Kubernetes cluster is not an API object, so its identity comes from the
// configured API server and its node count from the node list.
func (s *Source) syncCluster(ctx context.Context, results chan<- source.Data) error {
	nodes, err := s.listAllNodes(ctx)
	if err != nil {
		return err
	}

	return send(ctx, results, source.Data{
		Type:      clusterType,
		Operation: source.DataOperationUpsert,
		Values: map[string]any{
			"apiServer":   s.apiServer,
			"clusterName": s.clusterName,
			"nodeCount":   len(nodes),
		},
		Time: timeSource(),
	})
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
			namespace, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&list.Items[i])
			if err != nil {
				log.Error("error converting namespace, skipping", "namespace", list.Items[i].Name, "error", err.Error())
				continue
			}
			if metadata, ok := namespace["metadata"].(map[string]any); ok {
				delete(metadata, "managedFields")
			}

			err = send(ctx, results, source.Data{
				Type:      namespaceType,
				Operation: source.DataOperationUpsert,
				Values: map[string]any{
					"namespace": namespace,
					"apiServer": s.apiServer,
				},
				Time: timeSource(),
			})
			if err != nil {
				return err
			}
		}

		if list.Continue == "" {
			return nil
		}
		options.Continue = list.Continue
	}
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
