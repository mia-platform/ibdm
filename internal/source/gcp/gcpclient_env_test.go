// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package gcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewGCPInstance_Success(t *testing.T) {
	t.Setenv("GOOGLE_CLOUD_PUBSUB_PROJECT", "test-project-pubsub")
	t.Setenv("GOOGLE_CLOUD_PUBSUB_TOPIC", "topic-name")
	t.Setenv("GOOGLE_CLOUD_PUBSUB_SUBSCRIPTION", "sub-id")
	t.Setenv("GOOGLE_CLOUD_ASSET_PROJECT", "test-project-asset")

	inst, err := NewGCPSource(t.Context())
	require.NoError(t, err)
	require.NotNil(t, inst)

	if inst.p != nil {
		assert.Equal(t, "test-project-pubsub", inst.p.config.ProjectID)
		assert.Equal(t, "topic-name", inst.p.config.TopicName)
		assert.Equal(t, "sub-id", inst.p.config.SubscriptionID)
	}
	if inst.a != nil {
		assert.Equal(t, "test-project-asset", inst.a.config.ProjectID)
	}
}

func TestNewGCPInstance_MissingEnv(t *testing.T) {
	inst, err := NewGCPSource(t.Context())
	require.Error(t, err)
	require.Nil(t, inst)
}
