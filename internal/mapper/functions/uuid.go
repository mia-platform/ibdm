// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package functions

import "github.com/google/uuid"

// UUIDV4 generates a new UUID version 4.
func UUIDV4() (string, error) {
	id, err := uuid.NewRandom()
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

// UUIDV6 generates a new UUID version 6.
func UUIDV6() (string, error) {
	id, err := uuid.NewV6()
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

// UUIDV7 generates a new UUID version 7.
func UUIDV7() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", err
	}
	return id.String(), nil
}
