// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package logger

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogger(t *testing.T) {
	t.Parallel()

	buffer := new(bytes.Buffer)
	logger := NewLogger(buffer)

	logger.SetLevel(TRACE)
	namedLogger := logger.WithName("test_logger")
	namedLogger.Info("new log line for INFO level")
	logger.Trace("new log line for TRACE level")
	logger.SetLevel(DEBUG)
	logger.Debug("new log line for DEBUG level")
	namedLogger.Warn("new log line for WARN level")

	logger.SetLevel(ERROR)
	namedLogger.Warn("silenced log line for WARN level")
	logger.SetLevel(WARN)
	logger.Error("new log line for ERROR level")
	logger.Debug("silenced log line for TRACE level")

	logger.SetLevel(999) // invalid level; should default to INFO
	logger.Info("new log line for INFO level after invalid level set")
	namedLogger.Debug("silenced log line for DEBUG level after invalid level set")

	lines := strings.Split(buffer.String(), "\n")
	t.Logf("%v", lines)
	assert.Len(t, lines, 7) // 6 log lines plus 1 trailing empty line
}

func TestLevelStrings(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "TRACE", TRACE.String())
	assert.Equal(t, "DEBUG", DEBUG.String())
	assert.Equal(t, "INFO", INFO.String())
	assert.Equal(t, "WARN", WARN.String())
	assert.Equal(t, "ERROR", ERROR.String())
	assert.Equal(t, "Level(999)", Level(999).String())

	assert.Equal(t, TRACE, LevelFromString("TRACE"))
	assert.Equal(t, DEBUG, LevelFromString("DEBUG"))
	assert.Equal(t, INFO, LevelFromString("INFO"))
	assert.Equal(t, WARN, LevelFromString("WARN"))
	assert.Equal(t, ERROR, LevelFromString("ERROR"))
	assert.Equal(t, INFO, LevelFromString("INVALID"))
}

func TestLoggerWith(t *testing.T) {
	t.Parallel()

	buffer := new(bytes.Buffer)
	logger := NewLogger(buffer).With("requestId", "019f1cb8-0000-7000-8000-000000000004")
	logger.WithName("ibdm:source:fake").Info("renamed logger keeps the fields")

	var line map[string]any
	require.NoError(t, json.Unmarshal(buffer.Bytes(), &line))
	assert.Equal(t, "ibdm:source:fake", line["@module"])
	assert.Equal(t, "019f1cb8-0000-7000-8000-000000000004", line["requestId"])
}

func TestLoggerWithoutFields(t *testing.T) {
	t.Parallel()

	buffer := new(bytes.Buffer)
	NewLogger(buffer).WithName("ibdm:source:fake").Info("logged outside any request")

	var line map[string]any
	require.NoError(t, json.Unmarshal(buffer.Bytes(), &line))
	assert.Equal(t, "logged outside any request", line["@message"])
	assert.NotContains(t, line, "requestId", "a logger never given the field must not write it")
}
