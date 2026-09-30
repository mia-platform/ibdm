// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package k8s

import (
	"encoding/base64"
	"fmt"
	"net/url"

	"github.com/caarlos0/env/v11"
)

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
