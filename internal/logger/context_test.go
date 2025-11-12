// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package logger

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoggerInContext(t *testing.T) {
	t.Parallel()

	t.Run("from nil context return null logger", func(t *testing.T) {
		t.Parallel()
		var ctx context.Context = nil
		log := FromContext(ctx)
		assert.Equal(t, log, nullLogger)
	})

	t.Run("from empty context retunr null logger", func(t *testing.T) {
		t.Parallel()

		log := FromContext(t.Context())
		assert.Equal(t, log, nullLogger)
	})

	t.Run("context with a logger return that logger", func(t *testing.T) {
		t.Parallel()

		log := NewLogger(os.Stderr)
		ctx := WithContext(t.Context(), log)

		logFromCtx := FromContext(ctx)
		assert.Equal(t, logFromCtx, log)
	})
}
