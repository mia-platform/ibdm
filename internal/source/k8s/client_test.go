// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package k8s

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testKubeconfig = `apiVersion: v1
kind: Config
current-context: first
clusters:
  - name: first-cluster
    cluster:
      server: https://first.example.com
  - name: second-cluster
    cluster:
      server: https://second.example.com:6443
users:
  - name: my-user
    user:
      token: my-token
contexts:
  - name: first
    context:
      cluster: first-cluster
      user: my-user
  - name: second
    context:
      cluster: second-cluster
      user: my-user
`

func writeKubeconfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestBuildRestConfigTokenMode(t *testing.T) {
	t.Parallel()

	ca := []byte("my-ca-bundle")

	testCases := map[string]struct {
		cfg            sourceConfig
		expectCAData   []byte
		expectInsecure bool
	}{
		"with ca data": {
			cfg: sourceConfig{
				APIServer:   "https://api.example.com",
				BearerToken: "my-token",
				CACert:      base64.StdEncoding.EncodeToString(ca),
			},
			expectCAData: ca,
		},
		"with insecure skip verify": {
			cfg:            sourceConfig{APIServer: "https://api.example.com", BearerToken: "my-token", InsecureSkipVerify: true},
			expectInsecure: true,
		},
		"system trust store": {
			cfg: sourceConfig{APIServer: "https://api.example.com", BearerToken: "my-token"},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			restConfig, err := buildRestConfig(tc.cfg)
			require.NoError(t, err)
			assert.Equal(t, tc.cfg.APIServer, restConfig.Host)
			assert.Equal(t, "my-token", restConfig.BearerToken)
			assert.Equal(t, requestTimeout, restConfig.Timeout)
			assert.Equal(t, tc.expectInsecure, restConfig.Insecure)
			if tc.expectCAData == nil {
				assert.Empty(t, restConfig.CAData)
			} else {
				assert.Equal(t, tc.expectCAData, restConfig.CAData)
			}
		})
	}
}

func TestBuildRestConfigInvalidCA(t *testing.T) {
	t.Parallel()

	_, err := buildRestConfig(sourceConfig{APIServer: "https://api.example.com", BearerToken: "my-token", CACert: "%%%"})
	require.ErrorIs(t, err, ErrInvalidEnvVariable)
}

func TestBuildRestConfigKubeconfigMode(t *testing.T) {
	t.Parallel()

	path := writeKubeconfig(t, testKubeconfig)

	testCases := map[string]struct {
		cfg        sourceConfig
		expectHost string
		expectErr  error
	}{
		"current context": {
			cfg:        sourceConfig{KubeconfigPath: path},
			expectHost: "https://first.example.com",
		},
		"context override": {
			cfg:        sourceConfig{KubeconfigPath: path, KubeconfigContext: "second"},
			expectHost: "https://second.example.com:6443",
		},
		"unknown context": {
			cfg:       sourceConfig{KubeconfigPath: path, KubeconfigContext: "missing"},
			expectErr: ErrInvalidEnvVariable,
		},
		"missing file": {
			cfg:       sourceConfig{KubeconfigPath: filepath.Join(t.TempDir(), "missing")},
			expectErr: ErrInvalidEnvVariable,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			restConfig, err := buildRestConfig(tc.cfg)
			if tc.expectErr != nil {
				require.ErrorIs(t, err, tc.expectErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.expectHost, restConfig.Host)
			assert.Equal(t, requestTimeout, restConfig.Timeout)
		})
	}
}

func TestResolveClusterName(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		configured string
		apiServer  string
		expected   string
	}{
		"configured name wins":         {configured: "my-cluster", apiServer: "https://api.example.com", expected: "my-cluster"},
		"host without port":            {apiServer: "https://api.example.com", expected: "api.example.com"},
		"host with port":               {apiServer: "https://api.example.com:6443", expected: "api.example.com"},
		"unparsable falls back to raw": {apiServer: "https://%zz", expected: "%zz"},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, resolveClusterName(tc.configured, tc.apiServer))
		})
	}
}
