// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package k8s

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"k8s.io/client-go/kubernetes"

	"github.com/mia-platform/ibdm/internal/source"
)

var (
	// ErrK8sSource wraps all errors originating from the Kubernetes implementation.
	ErrK8sSource = errors.New("k8s source")
	// ErrMissingEnvVariable reports missing mandatory environment variables.
	ErrMissingEnvVariable = errors.New("missing environment variable")
	// ErrInvalidEnvVariable reports malformed environment variable values.
	ErrInvalidEnvVariable = errors.New("invalid environment value")
	// ErrRetrievingAssets wraps errors that occur while fetching API resources during sync.
	ErrRetrievingAssets = errors.New("error retrieving assets")
)

var _ source.SyncableSource = &Source{}

// Source implements source.SyncableSource for a Kubernetes cluster.
type Source struct {
	// apiServer is the API server URL of the monitored cluster, used to derive the cluster identity.
	apiServer string
	// clusterName is the human readable name of the cluster.
	clusterName string
	clientset   kubernetes.Interface

	syncLock sync.Mutex
}

// NewSource constructs a Source by reading its configuration from environment
// variables and building the Kubernetes clientset. It returns ErrK8sSource if
// the configuration is invalid or the client cannot be created.
func NewSource() (*Source, error) {
	cfg, err := loadSourceConfigFromEnv()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrK8sSource, err)
	}

	restConfig, err := buildRestConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrK8sSource, err)
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrK8sSource, err)
	}

	return &Source{
		apiServer:   restConfig.Host,
		clusterName: resolveClusterName(cfg.ClusterName, restConfig.Host),
		clientset:   clientset,
	}, nil
}

// handleErr converts errors leaving the package boundary: context cancellation
// is a normal shutdown and everything else is wrapped with ErrK8sSource.
func handleErr(err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil
	}

	return fmt.Errorf("%w: %w", ErrK8sSource, err)
}
