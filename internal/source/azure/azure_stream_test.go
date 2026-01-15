// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package azure

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStartStreamProcess(t *testing.T) {
}

func TestInvalidEventStreamProcess(t *testing.T) {
	t.Parallel()

	azureSource := &Source{
		config: config{},
	}

	err := azureSource.StartEventStream(t.Context(), nil, nil)
	assert.ErrorIs(t, err, ErrMissingEnvVariable)
}
