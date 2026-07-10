// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package jwk

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/caarlos0/env/v11"
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

// config holds the environment-driven settings consumed by LoadKeys.
type config struct {
	// CustomKeyID is an optional operator-provided key ID (kid). When empty, LoadKeys derives the
	// key ID automatically from the key material.
	CustomKeyID string `env:"CUSTOM_KEY_ID"`
}

// loadConfigFromEnv parses the LoadKeys configuration from environment variables.
func loadConfigFromEnv() (*config, error) {
	cfg := new(config)
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("cannot parse key configuration from environment: %w", err)
	}
	return cfg, nil
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

// LoadKeys reads the private key from privateKeyPath, builds the corresponding public JWKS, and
// returns the result. When a custom key ID is configured in the environment (see config) it is
// used as the key ID (kid) for both the public and private key; otherwise a key ID is derived
// automatically from the key material. It fails fast — any missing file, malformed PEM, or
// serialization error is returned as an error.
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

	cfg, err := loadConfigFromEnv()
	if err != nil {
		return nil, err
	}

	// Use the operator-provided key ID when set, otherwise derive one automatically from the key.
	if len(cfg.CustomKeyID) > 0 {
		if err := publicKey.Set(jwk.KeyIDKey, cfg.CustomKeyID); err != nil {
			return nil, fmt.Errorf("cannot set custom key ID on public key: %w", err)
		}
	} else if err := jwk.AssignKeyID(publicKey); err != nil {
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
