// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package mapper

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParsingError(t *testing.T) {
	underlyingErr := errors.New("underlying error")
	parsingErr := NewParsingError(underlyingErr)

	expectedMsg := "mapper template parsing error\nunderlying error"
	assert.Equal(t, expectedMsg, parsingErr.Error())
	result := errors.Is(parsingErr, underlyingErr)
	assert.True(t, result)
	result = errors.Is(parsingErr, parsingErr)
	assert.True(t, result)

	result = errors.Is(parsingErr, nil)
	assert.False(t, result)
	result = errors.Is(parsingErr, NewParsingError(nil))
	assert.False(t, result)

	assert.False(t, parsingErr.Is(nil))
}
