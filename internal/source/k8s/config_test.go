// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package k8s

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadSourceConfigFromEnvParseError(t *testing.T) {
	t.Setenv("K8S_API_SERVER", "https://api.example.com")
	t.Setenv("K8S_BEARER_TOKEN", "my-token")
	t.Setenv("K8S_INSECURE_SKIP_VERIFY", "not-a-bool")

	_, err := loadSourceConfigFromEnv()
	require.Error(t, err)
}

func TestLoadSourceConfigFromEnv(t *testing.T) {
	validCA := base64.StdEncoding.EncodeToString([]byte("my-ca-bundle"))

	testCases := map[string]struct {
		envVars   map[string]string
		expectErr error
		expectCfg sourceConfig
	}{
		"valid token mode": {
			envVars: map[string]string{
				"K8S_API_SERVER":   "https://api.example.com",
				"K8S_BEARER_TOKEN": "my-token",
			},
			expectCfg: sourceConfig{APIServer: "https://api.example.com", BearerToken: "my-token"},
		},
		"valid token mode with all options": {
			envVars: map[string]string{
				"K8S_API_SERVER":   "https://api.example.com:6443",
				"K8S_BEARER_TOKEN": "my-token",
				"K8S_CA_CERT":      validCA,
				"K8S_CLUSTER_NAME": "my-cluster",
			},
			expectCfg: sourceConfig{
				APIServer:   "https://api.example.com:6443",
				BearerToken: "my-token",
				CACert:      validCA,
				ClusterName: "my-cluster",
			},
		},
		"valid token mode with insecure skip verify": {
			envVars: map[string]string{
				"K8S_API_SERVER":           "https://api.example.com",
				"K8S_BEARER_TOKEN":         "my-token",
				"K8S_INSECURE_SKIP_VERIFY": "true",
			},
			expectCfg: sourceConfig{APIServer: "https://api.example.com", BearerToken: "my-token", InsecureSkipVerify: true},
		},
		"valid kubeconfig mode": {
			envVars:   map[string]string{"K8S_KUBECONFIG_PATH": "/tmp/kubeconfig"},
			expectCfg: sourceConfig{KubeconfigPath: "/tmp/kubeconfig"},
		},
		"valid kubeconfig mode with context and name": {
			envVars: map[string]string{
				"K8S_KUBECONFIG_PATH":    "/tmp/kubeconfig",
				"K8S_KUBECONFIG_CONTEXT": "my-context",
				"K8S_CLUSTER_NAME":       "my-cluster",
			},
			expectCfg: sourceConfig{KubeconfigPath: "/tmp/kubeconfig", KubeconfigContext: "my-context", ClusterName: "my-cluster"},
		},
		"nothing set": {
			envVars:   map[string]string{"K8S_CLUSTER_NAME": "my-cluster"},
			expectErr: ErrMissingEnvVariable,
		},
		"both modes set": {
			envVars: map[string]string{
				"K8S_API_SERVER":      "https://api.example.com",
				"K8S_BEARER_TOKEN":    "my-token",
				"K8S_KUBECONFIG_PATH": "/tmp/kubeconfig",
			},
			expectErr: ErrInvalidEnvVariable,
		},
		"token mode missing token": {
			envVars:   map[string]string{"K8S_API_SERVER": "https://api.example.com"},
			expectErr: ErrMissingEnvVariable,
		},
		"token mode missing api server": {
			envVars:   map[string]string{"K8S_BEARER_TOKEN": "my-token"},
			expectErr: ErrMissingEnvVariable,
		},
		"kubeconfig context without path": {
			envVars:   map[string]string{"K8S_KUBECONFIG_CONTEXT": "my-context"},
			expectErr: ErrMissingEnvVariable,
		},
		"invalid api server url": {
			envVars: map[string]string{
				"K8S_API_SERVER":   "api.example.com",
				"K8S_BEARER_TOKEN": "my-token",
			},
			expectErr: ErrInvalidEnvVariable,
		},
		"ca cert and insecure are exclusive": {
			envVars: map[string]string{
				"K8S_API_SERVER":           "https://api.example.com",
				"K8S_BEARER_TOKEN":         "my-token",
				"K8S_CA_CERT":              validCA,
				"K8S_INSECURE_SKIP_VERIFY": "true",
			},
			expectErr: ErrInvalidEnvVariable,
		},
		"ca cert not base64": {
			envVars: map[string]string{
				"K8S_API_SERVER":   "https://api.example.com",
				"K8S_BEARER_TOKEN": "my-token",
				"K8S_CA_CERT":      "%%%not-base64",
			},
			expectErr: ErrInvalidEnvVariable,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			for k, v := range tc.envVars {
				t.Setenv(k, v)
			}

			cfg, err := loadSourceConfigFromEnv()
			if tc.expectErr != nil {
				require.ErrorIs(t, err, tc.expectErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.expectCfg, cfg)
		})
	}
}
