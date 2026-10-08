// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package k8s

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/caarlos0/env/v11"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

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

// Source implements source.SyncableSource and source.EventSource for a Kubernetes cluster.
type Source struct {
	// apiServer is the API server URL of the monitored cluster, used to derive the cluster identity.
	apiServer string
	// clusterName is the human readable name of the cluster.
	clusterName string
	clientset   kubernetes.Interface
	// dynamic reads the CRD-backed kinds (ingressroute, certificate) as unstructured objects.
	dynamic dynamic.Interface

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

	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrK8sSource, err)
	}

	return &Source{
		apiServer:   restConfig.Host,
		clusterName: resolveClusterName(cfg.ClusterName, restConfig.Host),
		clientset:   clientset,
		dynamic:     dynamicClient,
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

const (
	// requestTimeout bounds every single request made to the API server.
	requestTimeout = 30 * time.Second
	// pageSize is the number of objects requested per List call.
	pageSize int64 = 500
)

// buildRestConfig creates the client configuration for the selected connection
// mode. Token mode builds the configuration directly, kubeconfig mode loads it
// from the configured file honouring the optional context override.
func buildRestConfig(cfg sourceConfig) (*rest.Config, error) {
	if cfg.hasKubeconfigMode() {
		return buildKubeconfigRestConfig(cfg)
	}

	caData, err := base64.StdEncoding.DecodeString(cfg.CACert)
	if err != nil {
		return nil, fmt.Errorf("%w: K8S_CA_CERT must be a base64 encoded PEM bundle", ErrInvalidEnvVariable)
	}

	return &rest.Config{
		Host:        cfg.APIServer,
		BearerToken: cfg.BearerToken,
		Timeout:     requestTimeout,
		TLSClientConfig: rest.TLSClientConfig{
			CAData:   caData,
			Insecure: cfg.InsecureSkipVerify,
		},
	}, nil
}

func buildKubeconfigRestConfig(cfg sourceConfig) (*rest.Config, error) {
	loadingRules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: cfg.KubeconfigPath}
	overrides := &clientcmd.ConfigOverrides{CurrentContext: cfg.KubeconfigContext}

	restConfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("%w: loading kubeconfig: %w", ErrInvalidEnvVariable, err)
	}

	restConfig.Timeout = requestTimeout
	return restConfig, nil
}

// resolveClusterName returns the configured cluster name or, when unset, the
// bare host (without scheme and port) of the API server.
func resolveClusterName(configured, apiServer string) string {
	if configured != "" {
		return configured
	}

	parsed, err := url.Parse(apiServer)
	if err != nil || parsed.Hostname() == "" {
		return strings.TrimPrefix(apiServer, "https://")
	}

	return parsed.Hostname()
}

// sourceConfig holds the environment-driven Kubernetes settings.
type sourceConfig struct {
	APIServer          string `env:"K8S_API_SERVER"`
	BearerToken        string `env:"K8S_BEARER_TOKEN"`
	CACert             string `env:"K8S_CA_CERT"`
	InsecureSkipVerify bool   `env:"K8S_INSECURE_SKIP_VERIFY"`
	ClusterName        string `env:"K8S_CLUSTER_NAME"`
	KubeconfigPath     string `env:"K8S_KUBECONFIG_PATH"`
	KubeconfigContext  string `env:"K8S_KUBECONFIG_CONTEXT"`
}

// loadSourceConfigFromEnv parses K8S_* environment variables into a sourceConfig.
func loadSourceConfigFromEnv() (sourceConfig, error) {
	cfg, err := env.ParseAs[sourceConfig]()
	if err != nil {
		return sourceConfig{}, err
	}
	if err := cfg.validate(); err != nil {
		return sourceConfig{}, err
	}
	return cfg, nil
}

// hasTokenMode reports whether any of the token mode settings is present.
func (c sourceConfig) hasTokenMode() bool {
	return c.APIServer != "" || c.BearerToken != "" || c.CACert != "" || c.InsecureSkipVerify
}

// hasKubeconfigMode reports whether any of the kubeconfig mode settings is present.
func (c sourceConfig) hasKubeconfigMode() bool {
	return c.KubeconfigPath != "" || c.KubeconfigContext != ""
}

// validate checks that the connection configuration is internally consistent:
// exactly one of token mode or kubeconfig mode must be configured.
func (c sourceConfig) validate() error {
	hasToken := c.hasTokenMode()
	hasKubeconfig := c.hasKubeconfigMode()

	switch {
	case !hasToken && !hasKubeconfig:
		return fmt.Errorf("%w: one of K8S_API_SERVER/K8S_BEARER_TOKEN or K8S_KUBECONFIG_PATH must be set",
			ErrMissingEnvVariable)
	case hasToken && hasKubeconfig:
		return fmt.Errorf("%w: K8S_API_SERVER/K8S_BEARER_TOKEN and K8S_KUBECONFIG_PATH are mutually exclusive",
			ErrInvalidEnvVariable)
	case hasKubeconfig:
		return c.validateKubeconfigMode()
	default:
		return c.validateTokenMode()
	}
}

func (c sourceConfig) validateKubeconfigMode() error {
	if c.KubeconfigPath == "" {
		return fmt.Errorf("%w: K8S_KUBECONFIG_PATH must be set when K8S_KUBECONFIG_CONTEXT is used", ErrMissingEnvVariable)
	}
	return nil
}

func (c sourceConfig) validateTokenMode() error {
	if c.APIServer == "" || c.BearerToken == "" {
		return fmt.Errorf("%w: K8S_API_SERVER and K8S_BEARER_TOKEN must both be set for token mode", ErrMissingEnvVariable)
	}

	parsed, err := url.Parse(c.APIServer)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return fmt.Errorf("%w: K8S_API_SERVER must be an http(s) URL", ErrInvalidEnvVariable)
	}

	if c.CACert != "" && c.InsecureSkipVerify {
		return fmt.Errorf("%w: K8S_CA_CERT and K8S_INSECURE_SKIP_VERIFY are mutually exclusive", ErrInvalidEnvVariable)
	}

	if c.CACert != "" {
		if _, err := base64.StdEncoding.DecodeString(c.CACert); err != nil {
			return fmt.Errorf("%w: K8S_CA_CERT must be a base64 encoded PEM bundle", ErrInvalidEnvVariable)
		}
	}

	return nil
}
