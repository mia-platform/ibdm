// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/config"
	"github.com/mia-platform/ibdm/internal/mapper"
	"github.com/mia-platform/ibdm/internal/pipeline"
)

func setupTestFileStructure(t *testing.T, baseDir string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Join(baseDir, "valid", "subdir"), os.ModePerm); err != nil {
		require.NoError(t, err)
	}

	if err := os.WriteFile(filepath.Join(baseDir, "valid", "invalid.yaml"), []byte("\tinvalid yaml file"), os.ModePerm); err != nil {
		require.NoError(t, err)
	}

	if err := os.Mkdir(filepath.Join(baseDir, "secret"), os.ModePerm); err != nil {
		require.NoError(t, err)
	}
	if err := os.Chmod(filepath.Join(baseDir, "secret"), 0o0000); err != nil {
		require.NoError(t, err)
	}
}

func TestExitWithErrorOutput(t *testing.T) {
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
			cmd := RunCmd()
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

func TestCompletion(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		args               []string
		toComplete         string
		expectedCompletion []string
	}{
		"no args, complete root commands": {
			args:               []string{},
			expectedCompletion: []string{"gcp\tGoogle Cloud Platform integration"},
		},
		"some args, no completions": {
			args: []string{"gcp"},
		},
	}

	for testName, test := range testCases {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			cmd := RunCmd()
			args, directive := validArgsFunc(cmd, test.args, test.toComplete)
			assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
			assert.Equal(t, test.expectedCompletion, args)
		})
	}
}

func TestOptionsRun(t *testing.T) {
	t.Parallel()

	sourceGetter = func(integrationName string) any {
		switch integrationName {
		case "fake":
			return &fakeSource{
				t: t,
			}
		case "unsupported":
			return "unsupported source type"
		}

		return nil
	}

	testCases := map[string]struct {
		options       *runOptions
		expectedError error
	}{
		"run integration and terminate when source return": {
			options: &runOptions{
				integrationName: "fake",
			},
		},
		"unsupported source type return error": {
			options: &runOptions{
				integrationName: "unsupported",
			},
			expectedError: errors.ErrUnsupported,
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

var _ pipeline.EventSource = &fakeSource{}

type fakeSource struct {
	t *testing.T
}

func (f *fakeSource) StartEventStream(ctx context.Context, types []string, out chan<- pipeline.SourceData) error {
	f.t.Helper()
	return nil
}
