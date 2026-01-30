// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package logger

import (
	"io"
	"strings"
	"time"

	"github.com/hashicorp/go-hclog"
)

var (
	// nullLogger discards every log entry.
	nullLogger = &instance{log: hclog.NewNullLogger()}
)

// Level enumerates the supported logging thresholds.
//
//go:generate ${TOOLS_BIN}/stringer -type=Level
type Level int

// LevelFromString parses a textual level and returns the matching Level value.
func LevelFromString(level string) Level {
	switch strings.ToUpper(level) {
	case "TRACE":
		return TRACE
	case "DEBUG":
		return DEBUG
	case "INFO":
		return INFO
	case "WARN":
		return WARN
	case "ERROR":
		return ERROR
	default:
		return INFO
	}
}

// convertedLevel maps the Level value to the corresponding hclog.Level.
func (l Level) convertedLevel() hclog.Level {
	switch l {
	case TRACE:
		return hclog.Trace
	case DEBUG:
		return hclog.Debug
	case INFO:
		return hclog.Info
	case WARN:
		return hclog.Warn
	case ERROR:
		return hclog.Error
	default:
		return hclog.Info
	}
}

const (
	ERROR Level = iota
	WARN
	INFO
	DEBUG
	TRACE
)

// Logger defines the logging surface exposed by this package.
type Logger interface {
	// WithName returns a logger namespaced with the provided component name.
	WithName(name string) Logger

	// SetLevel updates the logger level.
	SetLevel(level Level)

	// Trace emits a message and key/value pairs at the TRACE level.
	Trace(msg string, args ...any)

	// Debug emits a message and key/value pairs at the DEBUG level.
	Debug(msg string, args ...any)

	// Info emits a message and key/value pairs at the INFO level.
	Info(msg string, args ...any)

	// Warn emits a message and key/value pairs at the WARN level.
	Warn(msg string, args ...any)

	// Error emits a message and key/value pairs at the ERROR level.
	Error(msg string, args ...any)
}

// Make sure that instance satisfies Logger.
var _ Logger = &instance{}

// instance wraps an hclog.Logger implementation.
type instance struct {
	log hclog.Logger
}

// NewLogger creates a JSON logger writing to writer with INFO as the default level.
func NewLogger(writer io.Writer) Logger {
	return &instance{
		log: hclog.New(&hclog.LoggerOptions{
			JSONFormat: true,
			Output:     writer,
			TimeFn:     time.Now,
			Level:      INFO.convertedLevel(),
		}),
	}
}

func (i instance) WithName(name string) Logger {
	return &instance{
		log: i.log.ResetNamed(name),
	}
}

func (i instance) SetLevel(level Level) {
	i.log.SetLevel(level.convertedLevel())
}

func (i instance) Trace(msg string, args ...any) {
	i.log.Trace(msg, args...)
}

func (i instance) Debug(msg string, args ...any) {
	i.log.Debug(msg, args...)
}

func (i instance) Info(msg string, args ...any) {
	i.log.Info(msg, args...)
}

func (i instance) Warn(msg string, args ...any) {
	i.log.Warn(msg, args...)
}

func (i instance) Error(msg string, args ...any) {
	i.log.Error(msg, args...)
}
