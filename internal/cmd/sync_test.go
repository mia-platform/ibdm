// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/mia-platform/ibdm/internal/config"
	"github.com/mia-platform/ibdm/internal/mapper"
	"github.com/mia-platform/ibdm/internal/source/fake"
)

func TestSyncCmdErrorOutput(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	setupTestFileStructure(t, tmpDir)

	testCases := map[string]struct {
		args                 []string
		expectedError        error
		expectedUsage        bool
		expectedErrorMessage string
	}{
		"empty args, no error but usage output": {
			expectedUsage: true,
		},
		"unknown command, error returned and usage output": {
			args:                 []string{"unknown"},
			expectedError:        errInvalidIntegration,
			expectedErrorMessage: fmt.Sprintf("%s: unknown\n", errInvalidIntegration),
			expectedUsage:        true,
		},
		"missing mapping path, error returned no usage output": {
			args:                 []string{"gcp", "--" + mappingPathFlagName, filepath.Join(tmpDir, "missing")},
			expectedError:        syscall.ENOENT,
			expectedErrorMessage: fmt.Sprintf("mapping file %q: %s\n", filepath.Join(tmpDir, "missing"), syscall.ENOENT),
		},
		"error reading folder, error returned no usage output": {
			args:                 []string{"gcp", "--" + mappingPathFlagName, filepath.Join(tmpDir, "secret")},
			expectedError:        syscall.EACCES,
			expectedErrorMessage: fmt.Sprintf("mapping file %q: %s\n", filepath.Join(tmpDir, "secret"), syscall.EACCES),
		},
		"invalid mapping file, error returned no usage output": {
			args:                 []string{"gcp", "--" + mappingPathFlagName, filepath.Join(tmpDir, "valid")},
			expectedError:        config.ErrParsing,
			expectedErrorMessage: fmt.Sprintf("%s %q: %s\n", config.ErrParsing, filepath.Join(tmpDir, "valid", "invalid.yaml"), "yaml: found character that cannot start any token"),
		},
		"invalid templates, error returned no usage output": {
			args:                 []string{"gcp", "--" + mappingPathFlagName, filepath.Join("testdata", "invalid")},
			expectedError:        mapper.NewParsingError(errors.New("template: spec:2: function \"invalidFunc\" not defined")),
			expectedErrorMessage: mapper.NewParsingError(errors.New("template: spec:2: function \"invalidFunc\" not defined")).Error() + "\n",
		},
	}

	for testName, test := range testCases {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()
			cmd := SyncCmd()
			errBuffer := new(bytes.Buffer)
			outBuffer := new(bytes.Buffer)
			cmd.SetOut(outBuffer)
			cmd.SetErr(errBuffer)
			cmd.SetUsageTemplate("usage string")
			cmd.SetArgs(test.args)

			err := cmd.ExecuteContext(t.Context())
			if test.expectedError != nil {
				assert.ErrorIs(t, err, test.expectedError)
				assert.Equal(t, test.expectedErrorMessage, errBuffer.String())
			}

			if test.expectedUsage {
				assert.Equal(t, "usage string", outBuffer.String())
			}
		})
	}
}

func TestOptionsSync(t *testing.T) {
	t.Parallel()

	sourceGetter = func(_ context.Context, integrationName string) (any, error) {
		switch integrationName {
		case "fake":
			return fake.NewFakeSourceWithError(t, nil), nil
		case "unsupported":
			return "unsupported source type", nil
		}

		return nil, assert.AnError
	}

	testCases := map[string]struct {
		options       *syncOptions
		expectedError error
	}{
		"run integration and terminate when source return": {
			options: &syncOptions{
				integrationName: "fake",
			},
		},
		"unsupported source type return error": {
			options: &syncOptions{
				integrationName: "unsupported",
			},
			expectedError: errors.ErrUnsupported,
		},
		"error getting source return the error": {
			options: &syncOptions{
				integrationName: "unknown",
			},
			expectedError: assert.AnError,
		},
	}

	for testName, test := range testCases {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
			defer cancel()

			err := test.options.execute(ctx)
			if test.expectedError != nil {
				assert.ErrorIs(t, err, test.expectedError)
				return
			}
			assert.NoError(t, err)
		})
	}
}
