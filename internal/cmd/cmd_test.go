// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package cmd

import (
	"bytes"
	"fmt"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"

	"github.com/mia-platform/ibdm/internal/source/gcp"
)

func TestCmds(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		cmd                  *cobra.Command
		args                 []string
		expectedError        error
		expectedErrorMessage string
		expectedUsage        bool
	}{
		"run command with no arguments returns no error and print usage": {
			cmd:           RunCmd(),
			args:          []string{},
			expectedUsage: true,
		},
		"sync command with no arguments returns no error and print usage": {
			cmd:           SyncCmd(),
			args:          []string{},
			expectedUsage: true,
		},
		"run command missing path, return error no usage": {
			cmd:                  RunCmd(),
			args:                 []string{"--" + mappingPathFlagName, filepath.Join("testdata", "missing")},
			expectedError:        syscall.ENOENT,
			expectedErrorMessage: fmt.Sprintf("mapping file %q: %s\n", filepath.Join("testdata", "missing"), syscall.ENOENT),
			expectedUsage:        false,
		},
		"sync command missing path, return error no usage": {
			cmd:                  SyncCmd(),
			args:                 []string{"--" + mappingPathFlagName, filepath.Join("testdata", "missing")},
			expectedError:        syscall.ENOENT,
			expectedErrorMessage: fmt.Sprintf("mapping file %q: %s\n", filepath.Join("testdata", "missing"), syscall.ENOENT),
			expectedUsage:        false,
		},
		"run command return no error and no usage": {
			cmd:                  RunCmd(),
			args:                 []string{"invalid"},
			expectedUsage:        true,
			expectedError:        errInvalidIntegration,
			expectedErrorMessage: errInvalidIntegration.Error() + ": " + "invalid" + "\n",
		},
		"sync command return no error and no usage": {
			cmd:                  SyncCmd(),
			args:                 []string{"invalid"},
			expectedUsage:        true,
			expectedError:        errInvalidIntegration,
			expectedErrorMessage: errInvalidIntegration.Error() + ": " + "invalid" + "\n",
		},
		"run command return error when source return error": {
			cmd:                  RunCmd(),
			args:                 []string{"gcp"},
			expectedUsage:        false,
			expectedError:        gcp.ErrGCPSource,
			expectedErrorMessage: "gcp source: missing environment variable: GOOGLE_CLOUD_PUBSUB_PROJECT, GOOGLE_CLOUD_PUBSUB_SUBSCRIPTION\n",
		},
		"sync command return error when source return error": {
			cmd:                  SyncCmd(),
			args:                 []string{"gcp"},
			expectedUsage:        false,
			expectedError:        gcp.ErrGCPSource,
			expectedErrorMessage: "gcp source: missing environment variable: GOOGLE_CLOUD_SYNC_PARENT\n",
		},
	}

	for testName, test := range testCases {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			errBuffer := new(bytes.Buffer)
			outBuffer := new(bytes.Buffer)
			test.cmd.SetOut(outBuffer)
			test.cmd.SetErr(errBuffer)
			test.cmd.SetUsageTemplate("usage string")
			test.cmd.SetArgs(append(test.args, "--"+localOutputFlagName)) // force local output to avoid external dependencies

			err := test.cmd.ExecuteContext(t.Context())
			if test.expectedError != nil {
				assert.ErrorIs(t, err, test.expectedError)
				assert.Equal(t, test.expectedErrorMessage, errBuffer.String())
			} else {
				assert.NoError(t, err)
				assert.Empty(t, errBuffer)
			}

			if test.expectedUsage {
				assert.Equal(t, "usage string", outBuffer.String())
			} else {
				assert.Empty(t, outBuffer)
			}
		})
	}
}
