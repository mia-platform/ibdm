// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package functions

import "github.com/google/uuid"

func UUIDV4() (string, error) {
	id, err := uuid.NewRandom()
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

func UUIDV6() (string, error) {
	id, err := uuid.NewV6()
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

func UUIDV7() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", err
	}
	return id.String(), nil
}
