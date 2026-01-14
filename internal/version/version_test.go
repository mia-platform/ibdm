// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package version

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestServiceVersionInformation(t *testing.T) {
	// Save original values to restore them after tests
	originalVersion := Version
	originalBuildDate := BuildDate
	defer func() {
		Version = originalVersion
		BuildDate = originalBuildDate
	}()

	t.Run("default values", func(t *testing.T) {
		Version = "DEV"
		BuildDate = ""
		expected := "DEV, Go Version: " + runtime.Version()
		assert.Equal(t, expected, ServiceVersionInformation())
	})

	t.Run("with version only", func(t *testing.T) {
		Version = "1.0.0"
		BuildDate = ""
		expected := "1.0.0, Go Version: " + runtime.Version()
		assert.Equal(t, expected, ServiceVersionInformation())
	})

	t.Run("with version and build date", func(t *testing.T) {
		Version = "1.0.0"
		BuildDate = "2023-10-27"
		expected := "1.0.0 (2023-10-27), Go Version: " + runtime.Version()
		assert.Equal(t, expected, ServiceVersionInformation())
	})
}
