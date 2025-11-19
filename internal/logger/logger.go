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
	// nullLogger is a logger that discards all log messages.
	nullLogger = &instance{log: hclog.NewNullLogger()}
)

//go:generate ${TOOLS_BIN}/stringer -type=Level
type Level int

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

// Logger describes the interface that must be implemented by all loggers
type Logger interface {
	// WithName returns a new Logger instance with the specified name.
	WithName(name string) Logger

	// SetLevel updates the logger level.
	SetLevel(level Level)

	// Trace emit a message and key/value pairs at the TRACE level.
	Trace(msg string, args ...interface{})

	// Debug emit a message and key/value pairs at the DEBUG level.
	Debug(msg string, args ...interface{})

	// Info emit a message and key/value pairs at the INFO level.
	Info(msg string, args ...interface{})

	// Warn emit a message and key/value pairs at the WARN level.
	Warn(msg string, args ...interface{})

	// Error emit a message and key/value pairs at the ERROR level.
	Error(msg string, args ...interface{})
}

// Make sure that intLogger is a Logger.
var _ Logger = &instance{}

// instance is a Logger implementation.
type instance struct {
	log hclog.Logger
}

// NewLogger creates a new logger instance.
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

func (i instance) Trace(msg string, args ...interface{}) {
	i.log.Trace(msg, args...)
}

func (i instance) Debug(msg string, args ...interface{}) {
	i.log.Debug(msg, args...)
}

func (i instance) Info(msg string, args ...interface{}) {
	i.log.Info(msg, args...)
}

func (i instance) Warn(msg string, args ...interface{}) {
	i.log.Warn(msg, args...)
}

func (i instance) Error(msg string, args ...interface{}) {
	i.log.Error(msg, args...)
}
