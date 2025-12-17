// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package gcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckPubSubConfig(t *testing.T) {
	testCases := map[string]struct {
		setEnv               func(t *testing.T)
		expectedPubSubConfig pubSubConfig
		expectedErr          error
	}{
		"all env set": {
			setEnv: func(t *testing.T) {
				t.Helper()
				t.Setenv("GOOGLE_CLOUD_PUBSUB_PROJECT", "project-id")
				t.Setenv("GOOGLE_CLOUD_PUBSUB_SUBSCRIPTION", "subscription-id")
			},
			expectedPubSubConfig: pubSubConfig{
				ProjectID:      "project-id",
				SubscriptionID: "subscription-id",
			},
		},
		"missing one variable": {
			setEnv: func(t *testing.T) {
				t.Helper()
				t.Setenv("GOOGLE_CLOUD_PUBSUB_PROJECT", "project-id")
			},
			expectedErr: ErrMissingEnvVariable,
		},
		"missing all variables": {
			expectedErr: ErrMissingEnvVariable,
		},
	}

	for testName, test := range testCases {
		t.Run(testName, func(t *testing.T) {
			if test.setEnv != nil {
				test.setEnv(t)
			}

			source, err := NewSource()
			require.NoError(t, err)
			err = checkPubSubConfig(source.p.config)
			if test.expectedErr != nil {
				require.Error(t, err)
				assert.ErrorIs(t, err, test.expectedErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.expectedPubSubConfig, source.p.config)
		})
	}
}

func TestCheckAssetConfig(t *testing.T) {
	testCases := map[string]struct {
		setEnv              func(t *testing.T)
		expectedAssetConfig assetConfig
		expectedErr         error
	}{
		"all env set": {
			setEnv: func(t *testing.T) {
				t.Helper()
				t.Setenv("GOOGLE_CLOUD_SYNC_PARENT", "projects/project-id")
			},
			expectedAssetConfig: assetConfig{
				Parent: "projects/project-id",
			},
		},
		"all env set with organization parent": {
			setEnv: func(t *testing.T) {
				t.Helper()
				t.Setenv("GOOGLE_CLOUD_SYNC_PARENT", "organizations/12345")
			},
			expectedAssetConfig: assetConfig{
				Parent: "organizations/12345",
			},
		},
		"all env set with folder parent": {
			setEnv: func(t *testing.T) {
				t.Helper()
				t.Setenv("GOOGLE_CLOUD_SYNC_PARENT", "folders/12345")
			},
			expectedAssetConfig: assetConfig{
				Parent: "folders/12345",
			},
		},
		"wrong value for parent env": {
			setEnv: func(t *testing.T) {
				t.Helper()
				t.Setenv("GOOGLE_CLOUD_SYNC_PARENT", "invalid-parent-format")
			},
			expectedErr: ErrInvalidEnvVariable,
		},
		"missing all variables": {
			expectedErr: ErrMissingEnvVariable,
		},
	}

	for testName, test := range testCases {
		t.Run(testName, func(t *testing.T) {
			if test.setEnv != nil {
				test.setEnv(t)
			}

			source, err := NewSource()
			require.NoError(t, err)
			err = checkAssetConfig(source.a.config)
			if test.expectedErr != nil {
				require.Error(t, err)
				assert.ErrorIs(t, err, test.expectedErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.expectedAssetConfig, source.a.config)
		})
	}
}
