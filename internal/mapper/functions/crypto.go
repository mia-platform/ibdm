// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package functions

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
)

// Sha256Sum returns the SHA-256 digest of input encoded in hexadecimal.
func Sha256Sum(input string) string {
	hash := sha256.Sum256([]byte(input))
	return hex.EncodeToString(hash[:])
}

// Sha512Sum returns the SHA-512 digest of input encoded in hexadecimal.
func Sha512Sum(input string) string {
	hash := sha512.Sum512([]byte(input))
	return hex.EncodeToString(hash[:])
}
