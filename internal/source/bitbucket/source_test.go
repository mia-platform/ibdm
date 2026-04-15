// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package bitbucket

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSource(t *testing.T) {
	t.Setenv("BITBUCKET_ACCESS_TOKEN", "bbtoken123")

	s, err := NewSource()
	require.NoError(t, err)
	require.NotNil(t, s)
	assert.Equal(t, "bbtoken123", s.client.accessToken)
	assert.Equal(t, "https://api.bitbucket.org", s.client.baseURL)
	assert.Equal(t, "/bitbucket/webhook", s.webhookConfig.WebhookPath)
}

func TestNewSourceInvalidConfig(t *testing.T) {
	// No auth env vars set → validation error
	_, err := NewSource()
	require.ErrorIs(t, err, ErrBitbucketSource)
}
