// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package k8s

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
