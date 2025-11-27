// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package gcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewGCPInstance_Success(t *testing.T) {
	t.Setenv("GCP_PROJECT_ID", "test-project")
	t.Setenv("GCP_TOPIC_NAME", "topic-name")
	t.Setenv("GCP_SUBSCRIPTION_ID", "sub-id")

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
	inst, err := NewGCPInstance(t.Context())
	require.Error(t, err)
	require.Nil(t, inst)
}
