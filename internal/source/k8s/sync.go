// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package k8s

import (
	"context"
	"errors"
	"time"

	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/mia-platform/ibdm/internal/source"
)

const (
	loggerName = "ibdm:source:k8s"

	// clusterType is the data type key for the monitored cluster.
	clusterType = "cluster"

	// namespaceType is the data type key for Kubernetes namespaces.
	namespaceType = "namespace"

	// deploymentType is the data type key for Kubernetes deployments.
	deploymentType = "deployment"

	// statefulSetType is the data type key for Kubernetes statefulsets.
	statefulSetType = "statefulset"

	// daemonSetType is the data type key for Kubernetes daemonsets.
	daemonSetType = "daemonset"
)

// timeSource is a package-level function for the current time, replaceable in tests.
var timeSource = time.Now

// syncFunc syncs a single data type.
type syncFunc func(s *Source, ctx context.Context, results chan<- source.Data) error

// knownTypes lists the supported data types in the order they are synchronised.
var knownTypes = []struct {
	name string
	sync syncFunc
}{
	{name: clusterType, sync: (*Source).syncCluster},
	{name: namespaceType, sync: (*Source).syncNamespaces},
	{name: deploymentType, sync: (*Source).syncDeployments},
	{name: statefulSetType, sync: (*Source).syncStatefulSets},
	{name: daemonSetType, sync: (*Source).syncDaemonSets},
}

// StartSyncProcess performs a full synchronisation of the requested resource
// types by querying the Kubernetes API server and sending results to results.
// Unknown types are skipped with a debug log message. A failure in one type is
// logged and does not prevent the remaining types from being synchronised; the
// collected failures are returned at the end.
func (s *Source) StartSyncProcess(ctx context.Context, typesToSync map[string]source.Extra, results chan<- source.Data) error {
	log := logger.FromContext(ctx).WithName(loggerName)
	if !s.syncLock.TryLock() {
		log.Debug("sync process already running")
		return nil
	}
	defer s.syncLock.Unlock()

	for requested := range typesToSync {
		if !isKnownType(requested) {
			log.Debug("skipping unknown type", "type", requested)
		}
	}

	var errs []error
	for _, known := range knownTypes {
		if _, ok := typesToSync[known.name]; !ok {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil
		}

		log.Trace("syncing type", "type", known.name)
		if err := known.sync(s, ctx, results); err != nil {
			log.Error("error syncing type", "type", known.name, "error", err.Error())
			errs = append(errs, err)
		}
	}

	return handleErr(errors.Join(errs...))
}

// isKnownType reports whether name is a supported data type.
func isKnownType(name string) bool {
	for _, known := range knownTypes {
		if known.name == name {
			return true
		}
	}
	return false
}

// send pushes data onto results, giving up when the context is done.
func send(ctx context.Context, results chan<- source.Data, data source.Data) error {
	select {
	case results <- data:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
