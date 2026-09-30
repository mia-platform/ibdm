// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package k8s

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
	"time"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

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
