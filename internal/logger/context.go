// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package logger

import (
	"context"
)

// WithContext returns a new context with the provided logger.
func WithContext(ctx context.Context, logger Logger) context.Context {
	return context.WithValue(ctx, contextKey, logger)
}

// FromContext retrieves the logger from the context. If no logger is found, a new null logger is returned.
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

// contextKey is the key used for the context to store the logger.
var contextKey = contextKeyType{}
