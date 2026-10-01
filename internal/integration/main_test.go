// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

//go:build integration

package integration

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// modulePath is the package of the ibdm binary.
const modulePath = "github.com/mia-platform/ibdm"

// ibdmBinary is the path of the binary built by TestMain for every test of the package.
var ibdmBinary string

// TestMain builds the ibdm binary once, runs the tests, and removes the binary.
func TestMain(m *testing.M) {
	os.Exit(buildAndRun(m))
}

// buildAndRun builds the binary into a temporary directory and runs the tests. A failed build
// fails the whole package with the compiler output.
func buildAndRun(m *testing.M) int {
	dir, err := os.MkdirTemp("", "ibdm-integration-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "creating the build directory: %v\n", err)
		return 1
	}
	defer os.RemoveAll(dir)

	binaryName := "ibdm"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	ibdmBinary = filepath.Join(dir, binaryName)

	build := exec.Command("go", "build", "-race", "-o", ibdmBinary, modulePath)
	if output, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "building %s: %v\n%s", modulePath, err, output)
		return 1
	}

	return m.Run()
}
