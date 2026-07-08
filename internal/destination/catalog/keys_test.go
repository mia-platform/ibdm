// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package catalog

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadKeysSuccess(t *testing.T) {
	t.Parallel()

	key := generateTestRSAKey(t)
	path := filepath.Join(t.TempDir(), "private-key.pem")
	writeTestFile(t, path, encodePKCS8PEM(t, key))

	keys, err := LoadKeys(path)
	require.NoError(t, err)
	require.NotNil(t, keys)
	require.NotNil(t, keys.PrivateKey)

	kid, ok := keys.PrivateKey.KeyID()
	require.True(t, ok)
	assert.NotEmpty(t, kid)

	var jwks struct {
		Keys []map[string]any `json:"keys"`
	}
	require.NoError(t, json.Unmarshal(keys.JWKSBytes, &jwks))
	require.Len(t, jwks.Keys, 1)
	assert.Equal(t, "sig", jwks.Keys[0]["use"])
	assert.Equal(t, kid, jwks.Keys[0]["kid"])
	assert.NotContains(t, jwks.Keys[0], "d", "the JWKS must only expose the public key")
}

func TestLoadKeysMissingFile(t *testing.T) {
	t.Parallel()

	_, err := LoadKeys(filepath.Join(t.TempDir(), "does-not-exist.pem"))
	require.Error(t, err)
	assert.ErrorContains(t, err, "cannot read private key from")
}

func TestLoadKeysInvalidPEM(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "invalid-key.pem")
	writeTestFile(t, path, "not-a-valid-pem-key")

	_, err := LoadKeys(path)
	require.Error(t, err)
	assert.ErrorContains(t, err, "cannot parse private key PEM")
}
