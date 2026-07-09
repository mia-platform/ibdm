// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package jwk

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// MockRSAPEM generates a test-only private key PEM.
func MockRSAPEM(tb testing.TB) string {
	tb.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		tb.Fatalf("cannot generate mock RSA key: %v", err)
	}

	pkcs8Bytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		tb.Fatalf("cannot marshal mock RSA key: %v", err)
	}

	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8Bytes})
	if pemBytes == nil {
		tb.Fatal("cannot encode mock RSA key to PEM")
	}

	return string(pemBytes)
}

func TestLoadKeys(t *testing.T) {
	t.Run("loads RSA key from file", func(t *testing.T) {
		f, err := os.CreateTemp(t.TempDir(), "key-*.pem")
		require.NoError(t, err)
		_, err = f.WriteString(MockRSAPEM(t))
		require.NoError(t, err)
		require.NoError(t, f.Close())

		keys, err := LoadKeys(f.Name())
		require.NoError(t, err)

		require.NotNil(t, keys.PrivateKey)
		require.NotEmpty(t, keys.JWKSBytes)

		// JWKS must be valid JSON with a "keys" array.
		var jwks map[string]any
		require.NoError(t, json.Unmarshal(keys.JWKSBytes, &jwks))
		keysArr, ok := jwks["keys"].([]any)
		require.True(t, ok, "JWKS must have a 'keys' array")
		require.Len(t, keysArr, 1)

		// The single key must carry a kid and have use=sig.
		entry := keysArr[0].(map[string]any)
		require.NotEmpty(t, entry["kid"], "public key must have a kid")
		require.Equal(t, "sig", entry["use"])

		// The private key must have the same kid as the public key in the JWKS.
		kid, ok := keys.PrivateKey.KeyID()
		require.True(t, ok)
		require.Equal(t, entry["kid"], kid)
	})

	t.Run("missing file returns error", func(t *testing.T) {
		_, err := LoadKeys("/nonexistent/key.pem")
		require.ErrorContains(t, err, "cannot read private key")
	})

	t.Run("invalid PEM returns error", func(t *testing.T) {
		f, err := os.CreateTemp(t.TempDir(), "key-*.pem")
		require.NoError(t, err)
		_, err = f.WriteString("not a valid pem")
		require.NoError(t, err)
		require.NoError(t, f.Close())

		_, err = LoadKeys(f.Name())
		require.ErrorContains(t, err, "cannot parse private key PEM")
	})
}
