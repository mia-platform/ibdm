// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package gcp

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewGCPInstance_Success(t *testing.T) {
	oldProject := os.Getenv("GCP_PROJECT_ID")
	oldTopic := os.Getenv("GCP_TOPIC_NAME")
	oldSub := os.Getenv("GCP_SUBSCRIPTION_ID")
	defer func() {
		_ = os.Setenv("GCP_PROJECT_ID", oldProject)
		_ = os.Setenv("GCP_TOPIC_NAME", oldTopic)
		_ = os.Setenv("GCP_SUBSCRIPTION_ID", oldSub)
	}()

	require.NoError(t, os.Setenv("GCP_PROJECT_ID", "test-project"))
	require.NoError(t, os.Setenv("GCP_TOPIC_NAME", "topic-name"))
	require.NoError(t, os.Setenv("GCP_SUBSCRIPTION_ID", "sub-id"))

	inst, err := NewGCPInstance(t.Context())
	require.NoError(t, err)
	require.NotNil(t, inst)

	if inst.p != nil {
		assert.Equal(t, "test-project", inst.p.config.ProjectID)
		assert.Equal(t, "topic-name", inst.p.config.TopicName)
		assert.Equal(t, "sub-id", inst.p.config.SubscriptionID)
	}
	if inst.a != nil {
		assert.Equal(t, "test-project", inst.a.config.ProjectID)
	}
}

func TestNewGCPInstance_MissingEnv(t *testing.T) {
	oldProject := os.Getenv("GCP_PROJECT_ID")
	oldTopic := os.Getenv("GCP_TOPIC_NAME")
	oldSub := os.Getenv("GCP_SUBSCRIPTION_ID")
	defer func() {
		_ = os.Setenv("GCP_PROJECT_ID", oldProject)
		_ = os.Setenv("GCP_TOPIC_NAME", oldTopic)
		_ = os.Setenv("GCP_SUBSCRIPTION_ID", oldSub)
	}()

	_ = os.Unsetenv("GCP_PROJECT_ID")
	_ = os.Unsetenv("GCP_TOPIC_NAME")
	_ = os.Unsetenv("GCP_SUBSCRIPTION_ID")

	inst, err := NewGCPInstance(t.Context())
	require.Error(t, err)
	require.Nil(t, inst)
}
