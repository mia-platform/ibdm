// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package functions

import "github.com/google/uuid"

// UUIDV4 returns a randomly generated (version 4) UUID string.
func UUIDV4() (string, error) {
	id, err := uuid.NewRandom()
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

// UUIDV6 returns a time-ordered (version 6) UUID string.
func UUIDV6() (string, error) {
	id, err := uuid.NewV6()
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

// UUIDV7 returns a monotonic (version 7) UUID string.
func UUIDV7() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", err
	}
	return id.String(), nil
}
