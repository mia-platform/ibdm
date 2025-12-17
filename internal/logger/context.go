// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package logger

import (
	"context"
)

// WithContext returns ctx carrying logger.
func WithContext(ctx context.Context, logger Logger) context.Context {
	return context.WithValue(ctx, contextKey, logger)
}

// FromContext extracts the logger stored in ctx, falling back to the null logger.
func FromContext(ctx context.Context) Logger {
	if ctx != nil {
		if logger, ok := ctx.Value(contextKey).(Logger); ok {
			return logger
		}
	}

	return nullLogger
}

// Unexported new type so that our context key never collides with another.
type contextKeyType struct{}

// contextKey stores the logger in the context.
var contextKey = contextKeyType{}
