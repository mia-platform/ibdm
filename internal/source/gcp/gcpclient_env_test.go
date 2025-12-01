// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package gcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckPubSubConfig_Success(t *testing.T) {
	t.Setenv("GOOGLE_CLOUD_PUBSUB_PROJECT", "test-project-pubsub")
	t.Setenv("GOOGLE_CLOUD_PUBSUB_TOPIC", "topic-name")
	t.Setenv("GOOGLE_CLOUD_PUBSUB_SUBSCRIPTION", "sub-id")
	t.Setenv("GOOGLE_CLOUD_ASSET_PROJECT", "test-project-asset")

	src, err := NewGCPSource(t.Context())
	require.NoError(t, err)
	require.NotNil(t, src)

	err = checkPubSubConfig(src.p.config)
	require.NoError(t, err)

	assert.Equal(t, "test-project-pubsub", src.p.config.ProjectID)
	assert.Equal(t, "topic-name", src.p.config.TopicName)
	assert.Equal(t, "sub-id", src.p.config.SubscriptionID)
}

func TestCheckAssetConfig_Success(t *testing.T) {
	t.Setenv("GOOGLE_CLOUD_ASSET_PROJECT", "test-project-asset")
	t.Setenv("GOOGLE_CLOUD_ASSET_PARENT", "projects/test-project-asset")

	src, err := NewGCPSource(t.Context())
	require.NoError(t, err)
	require.NotNil(t, src)

	err = checkAssetConfig(src.a.config)
	require.NoError(t, err)

	assert.Equal(t, "test-project-asset", src.a.config.ProjectID)
	assert.Equal(t, "projects/test-project-asset", src.a.config.Parent)
}

func TestCheckAssetConfig_Fail_WrongParent(t *testing.T) {
	t.Setenv("GOOGLE_CLOUD_ASSET_PROJECT", "test-project-asset")
	t.Setenv("GOOGLE_CLOUD_ASSET_PARENT", "organization/test-project-asset")

	src, err := NewGCPSource(t.Context())
	require.NoError(t, err)
	require.NotNil(t, src)

	err = checkAssetConfig(src.a.config)
	require.Error(t, err)
}

func TestCheckPubSubConfig_MissingEnv(t *testing.T) {
	src, err := NewGCPSource(t.Context())
	require.NoError(t, err)
	require.NotNil(t, src)

	err = checkPubSubConfig(src.p.config)
	require.Error(t, err)
}

func TestCheckAssetConfig_MissingEnv(t *testing.T) {
	src, err := NewGCPSource(t.Context())
	require.NoError(t, err)
	require.NotNil(t, src)

	err = checkAssetConfig(src.a.config)
	require.Error(t, err)
}
