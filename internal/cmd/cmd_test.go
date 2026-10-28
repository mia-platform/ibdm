// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package cmd

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/mappings"
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
			args:                 []string{"--" + mappingFileFlagName, filepath.Join("testdata", "missing")},
			expectedError:        syscall.ENOENT,
			expectedErrorMessage: fmt.Sprintf("mapping file %q: %s\n", filepath.Join("testdata", "missing"), syscall.ENOENT),
			expectedUsage:        false,
		},
		"sync command missing path, return error no usage": {
			cmd:                  SyncCmd(),
			args:                 []string{"--" + mappingFileFlagName, filepath.Join("testdata", "missing")},
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
		"run command with no mapping selected exits without building the source": {
			cmd:  RunCmd(),
			args: []string{"gcp"},
		},
		"sync command with no mapping selected exits without building the source": {
			cmd:  SyncCmd(),
			args: []string{"gcp"},
		},
		"run command return error when source return error": {
			cmd:                  RunCmd(),
			args:                 []string{"gcp", "--" + includeInternalMappingsFlagName + "=all"},
			expectedUsage:        false,
			expectedError:        gcp.ErrGCPSource,
			expectedErrorMessage: "gcp source: missing environment variable: GOOGLE_CLOUD_PUBSUB_PROJECT, GOOGLE_CLOUD_PUBSUB_SUBSCRIPTION\n",
		},
		"sync command return error when source return error": {
			cmd:                  SyncCmd(),
			args:                 []string{"gcp", "--" + includeInternalMappingsFlagName + "=all"},
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
			test.cmd.SetArgs(append(test.args, "--"+localOutputFlagName)) // forces local output to avoid external dependencies

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

func TestMappingsListCmd(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		args           []string
		expectedOutput string
		expectedError  error
		expectedUsage  bool
	}{
		"lists the internal mappings of an integration": {
			args:           []string{"list", "nexus"},
			expectedOutput: "dockerimages\n",
		},
		"integration names are case insensitive": {
			args:           []string{"list", "NEXUS"},
			expectedOutput: "dockerimages\n",
		},
		"an unknown integration is an error": {
			args:          []string{"list", "unknown"},
			expectedError: mappings.ErrUnknownSource,
		},
		"no integration prints the usage": {
			args:          []string{"list"},
			expectedUsage: true,
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			outBuffer, errBuffer := new(bytes.Buffer), new(bytes.Buffer)
			cmd := MappingsCmd()
			cmd.SetOut(outBuffer)
			cmd.SetErr(errBuffer)
			for _, subcommand := range cmd.Commands() {
				subcommand.SetUsageTemplate("usage string")
			}
			cmd.SetArgs(test.args)

			err := cmd.ExecuteContext(t.Context())
			if test.expectedError != nil {
				require.ErrorIs(t, err, test.expectedError)
				require.Contains(t, errBuffer.String(), "azure, azure-devops")
				return
			}

			require.NoError(t, err)
			if test.expectedUsage {
				require.Equal(t, "usage string", outBuffer.String())
				return
			}
			require.Equal(t, test.expectedOutput, outBuffer.String())
		})
	}
}

// TestIntegrationsMatchInternalMappings keeps the CLI integrations and the internal mapping
// registry in step: an integration is a system source iff ibdm ships mappings for it.
func TestIntegrationsMatchInternalMappings(t *testing.T) {
	t.Parallel()

	require.Equal(t, mappings.Sources(), slices.Sorted(maps.Keys(availableSyncSources)))
	require.Equal(t, mappings.Sources(), slices.Sorted(maps.Keys(availableEventSources)))
}

func TestInternalMappingsCompletion(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		withAll    bool
		args       []string
		toComplete string
		expected   []string
	}{
		"include offers all and every name": {
			withAll:  true,
			args:     []string{"gitlab"},
			expected: []string{"all", "accesstokens", "pipelines", "projects"},
		},
		"exclude does not offer all": {
			args:     []string{"gitlab"},
			expected: []string{"accesstokens", "pipelines", "projects"},
		},
		"a prefix narrows the names": {
			withAll:    true,
			args:       []string{"gitlab"},
			toComplete: "p",
			expected:   []string{"pipelines", "projects"},
		},
		"the last element of a list is completed without repeating the chosen ones": {
			withAll:    true,
			args:       []string{"gitlab"},
			toComplete: "projects,",
			expected:   []string{"projects,accesstokens", "projects,pipelines"},
		},
		"no integration gives no completion": {
			withAll:  true,
			expected: nil,
		},
		"an unknown integration gives no completion": {
			withAll:  true,
			args:     []string{"unknown"},
			expected: nil,
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			completions, directive := internalMappingsCompletion(test.withAll)(nil, test.args, test.toComplete)
			require.Equal(t, test.expected, completions)
			require.NotZero(t, directive&cobra.ShellCompDirectiveNoFileComp)
		})
	}
}

func TestMappingsShowCmd(t *testing.T) {
	t.Parallel()

	runShow := func(t *testing.T, args ...string) (string, string, error) {
		t.Helper()

		outBuffer, errBuffer := new(bytes.Buffer), new(bytes.Buffer)
		cmd := MappingsCmd()
		cmd.SetOut(outBuffer)
		cmd.SetErr(errBuffer)
		for _, subcommand := range cmd.Commands() {
			subcommand.SetUsageTemplate("usage string")
		}
		cmd.SetArgs(append([]string{"show"}, args...))
		err := cmd.ExecuteContext(t.Context())
		return outBuffer.String(), errBuffer.String(), err
	}

	t.Run("every internal mapping is printed byte for byte", func(t *testing.T) {
		t.Parallel()

		for _, source := range mappings.Sources() {
			names, err := mappings.Names(source)
			require.NoError(t, err)
			for _, name := range names {
				expected, err := os.ReadFile(filepath.Join("..", "mappings", "data", source, name+".yaml"))
				require.NoError(t, err)

				output, _, err := runShow(t, source, name)
				require.NoError(t, err)
				require.Equal(t, string(expected), output, "%s/%s", source, name)
			}
		}
	})

	t.Run("an unknown mapping is an error listing the valid names", func(t *testing.T) {
		t.Parallel()
		_, errOutput, err := runShow(t, "nexus", "unknown")
		require.ErrorIs(t, err, mappings.ErrUnknownMapping)
		require.Contains(t, errOutput, "internal mappings are dockerimages")
	})

	t.Run("an unknown integration is an error", func(t *testing.T) {
		t.Parallel()
		_, _, err := runShow(t, "unknown", "projects")
		require.ErrorIs(t, err, mappings.ErrUnknownSource)
	})

	t.Run("missing arguments print the usage", func(t *testing.T) {
		t.Parallel()
		output, _, err := runShow(t, "nexus")
		require.NoError(t, err)
		require.Equal(t, "usage string", output)
	})

	t.Run("completion offers integrations then mapping names", func(t *testing.T) {
		t.Parallel()

		show := mappingsShowCmd()
		integrations, _ := show.ValidArgsFunction(show, nil, "")
		require.Equal(t, mappings.Sources(), integrations)

		names, _ := show.ValidArgsFunction(show, []string{"gitlab"}, "")
		require.Equal(t, []string{"accesstokens", "pipelines", "projects"}, names)

		none, _ := show.ValidArgsFunction(show, []string{"gitlab", "projects"}, "")
		require.Empty(t, none)
	})
}
