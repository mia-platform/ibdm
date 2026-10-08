// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package k8s

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"github.com/mia-platform/ibdm/internal/source"
)

func TestNewSource(t *testing.T) {
	t.Run("token mode", func(t *testing.T) {
		t.Setenv("K8S_API_SERVER", "https://api.example.com:6443")
		t.Setenv("K8S_BEARER_TOKEN", "my-token")

		s, err := NewSource()
		require.NoError(t, err)
		require.NotNil(t, s)
		assert.Equal(t, "https://api.example.com:6443", s.apiServer)
		assert.Equal(t, "api.example.com", s.clusterName)
		assert.NotNil(t, s.clientset)
	})

	t.Run("token mode with cluster name", func(t *testing.T) {
		t.Setenv("K8S_API_SERVER", "https://api.example.com")
		t.Setenv("K8S_BEARER_TOKEN", "my-token")
		t.Setenv("K8S_CLUSTER_NAME", "my-cluster")

		s, err := NewSource()
		require.NoError(t, err)
		assert.Equal(t, "my-cluster", s.clusterName)
	})

	t.Run("kubeconfig mode uses the kubeconfig host", func(t *testing.T) {
		t.Setenv("K8S_KUBECONFIG_PATH", writeKubeconfig(t, testKubeconfig))
		t.Setenv("K8S_KUBECONFIG_CONTEXT", "second")

		s, err := NewSource()
		require.NoError(t, err)
		assert.Equal(t, "https://second.example.com:6443", s.apiServer)
		assert.Equal(t, "second.example.com", s.clusterName)
	})

	t.Run("invalid configuration", func(t *testing.T) {
		s, err := NewSource()
		require.ErrorIs(t, err, ErrK8sSource)
		require.ErrorIs(t, err, ErrMissingEnvVariable)
		assert.Nil(t, s)
	})

	t.Run("unreadable kubeconfig", func(t *testing.T) {
		t.Setenv("K8S_KUBECONFIG_PATH", t.TempDir()+"/missing")

		s, err := NewSource()
		require.ErrorIs(t, err, ErrK8sSource)
		assert.Nil(t, s)
	})
}

func TestHandleErr(t *testing.T) {
	t.Parallel()

	someErr := errors.New("boom")

	require.NoError(t, handleErr(nil))
	require.NoError(t, handleErr(context.Canceled))
	require.NoError(t, handleErr(context.DeadlineExceeded))

	err := handleErr(someErr)
	require.ErrorIs(t, err, ErrK8sSource)
	require.ErrorIs(t, err, someErr)
}

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

const (
	testAPIServer   = "https://api.my-cluster.example.com:6443"
	testClusterName = "my-cluster"
)

// testFixedTime is the canonical fixed time used across all time-sensitive tests.
var testFixedTime = time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)

func setupFixedTime(t *testing.T) {
	t.Helper()
	originalTimeSource := timeSource
	t.Cleanup(func() { timeSource = originalTimeSource })
	timeSource = func() time.Time { return testFixedTime }
}

// newFakeSource creates a Source backed by the fake clientset preloaded with objects.
func newFakeSource(t *testing.T, objects ...runtime.Object) *Source {
	t.Helper()
	return &Source{
		apiServer:   testAPIServer,
		clusterName: testClusterName,
		clientset:   fake.NewClientset(objects...),
	}
}

// newHTTPSource creates a Source whose real clientset talks to an httptest server,
// so that query parameters such as the pagination continue token are observable.
func newHTTPSource(t *testing.T, handler http.Handler) *Source {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	clientset, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
	require.NoError(t, err)

	return &Source{
		apiServer:   testAPIServer,
		clusterName: testClusterName,
		clientset:   clientset,
	}
}

// writeJSON writes body as a JSON response.
func writeJSON(t *testing.T, w http.ResponseWriter, body any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(body))
}

func newNode(name string) corev1.Node {
	return corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name}}
}

func newNamespace(name string, labels map[string]string) *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}}
}

// collectData drains a closed results channel.
func collectData(results <-chan source.Data) []source.Data {
	var items []source.Data
	for d := range results {
		items = append(items, d)
	}
	return items
}
