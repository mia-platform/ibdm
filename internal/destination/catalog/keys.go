// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package catalog

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/lestrrat-go/jwx/v3/jwk"
)

// Keys holds the private key used to sign JWT assertions and the corresponding public JWKS
// document, derived from it by LoadKeys.
type Keys struct {
	// PrivateKey is used to sign the JWT assertion sent to the token endpoint.
	PrivateKey jwk.Key
	// JWKSBytes is the JSON-serialized public JWK set matching PrivateKey.
	JWKSBytes []byte
}

// LoadKeys reads the private key from the source described by cfg, builds the
// corresponding public JWKS, and returns the result. It fails fast — any
// missing file, malformed PEM, or serialization error is returned as an error.
func LoadKeys(privateKeyPath string) (*Keys, error) {
	pemBytes, err := readKeyMaterial(privateKeyPath)
	if err != nil {
		return nil, err
	}

	privateKey, err := jwk.ParseKey(pemBytes, jwk.WithPEM(true))
	if err != nil {
		return nil, fmt.Errorf("cannot parse private key PEM: %w", err)
	}

	publicKey, err := jwk.PublicKeyOf(privateKey)
	if err != nil {
		return nil, fmt.Errorf("cannot derive public key from private key: %w", err)
	}

	if err := publicKey.Set(jwk.KeyUsageKey, "sig"); err != nil {
		return nil, fmt.Errorf("cannot set key usage on public key: %w", err)
	}

	if err := jwk.AssignKeyID(publicKey); err != nil {
		return nil, fmt.Errorf("cannot assign key ID to public key: %w", err)
	}

	kid, _ := publicKey.KeyID()
	if err := privateKey.Set(jwk.KeyIDKey, kid); err != nil {
		return nil, fmt.Errorf("cannot set key ID on private key: %w", err)
	}

	set := jwk.NewSet()
	if err := set.AddKey(publicKey); err != nil {
		return nil, fmt.Errorf("cannot add public key to JWK set: %w", err)
	}

	jwksBytes, err := json.Marshal(set)
	if err != nil {
		return nil, fmt.Errorf("cannot serialize JWKS to JSON: %w", err)
	}

	return &Keys{
		PrivateKey: privateKey,
		JWKSBytes:  jwksBytes,
	}, nil
}

// readKeyMaterial resolves the raw PEM bytes from the configured source.
// "file": reads from the path mounted as a Kubernetes Secret volume.
func readKeyMaterial(privateKeyPath string) ([]byte, error) {
	data, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read private key from %q: %w", privateKeyPath, err)
	}
	return data, nil
}
