// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

//go:build integration

package integration

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	cliMappingsDir = "testdata/mappings/cli"

	levelInfo = "info"
	levelWarn = "warn"

	githubNoOpMessage = "github: no mappings selected, nothing to do"
)

// githubEnv points the GitHub source at upstream, and the Catalog destination at catalog.
func githubEnv(upstream *fakeUpstream, catalog *fakeCatalog) map[string]string {
	env := catalog.env()
	env["GITHUB_URL"] = upstream.baseURL()
	env["GITHUB_TOKEN"] = "test-token"
	env["GITHUB_ORG"] = "acme-widgets"
	env["GITHUB_WEBHOOK_SECRET"] = "test-webhook-secret"
	return env
}

// cliMapping returns the path of a CLI scenario mapping file.
func cliMapping(name string) string {
	return filepath.Join(cliMappingsDir, name)
}

// TestCLIStopsBeforeAnyTraffic covers the selection and the startup refusals of Self-Seeded
// Mappings end to end. Every scenario ends before a source or a destination is used: the GitHub
// fake has no route, so any request fails the test, and the fake Catalog must receive nothing.
func TestCLIStopsBeforeAnyTraffic(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		args             []string
		expectedExitCode int
		expectedLogs     [][2]string
		expectedStderr   []string
	}{
		"sync with nothing selected does nothing": {
			args:         []string{"sync", "github"},
			expectedLogs: [][2]string{{levelInfo, githubNoOpMessage}},
		},
		"run with nothing selected returns without starting the server": {
			args:         []string{"run", "github"},
			expectedLogs: [][2]string{{levelInfo, githubNoOpMessage}},
		},
		"exclude alone warns and does nothing": {
			args: []string{"sync", "github", "--exclude-internal-mappings=workflowruns"},
			expectedLogs: [][2]string{
				{levelWarn, "--exclude-internal-mappings without --include-internal-mappings selects no internal mapping"},
				{levelInfo, githubNoOpMessage},
			},
		},
		"unknown internal name is refused with the valid names": {
			args:             []string{"sync", "github", "--include-internal-mappings=nope"},
			expectedExitCode: 1,
			expectedStderr:   []string{`unknown internal mapping "nope"`, "repositories, workflowruns"},
		},
		"all mixed with names is refused": {
			args:             []string{"sync", "github", "--include-internal-mappings=all,repositories"},
			expectedExitCode: 1,
			expectedStderr:   []string{"'all' cannot be combined with other mapping names"},
		},
		"exclude all is refused": {
			args:             []string{"sync", "github", "--include-internal-mappings=all", "--exclude-internal-mappings=all"},
			expectedExitCode: 1,
			expectedStderr:   []string{"'all' is not a valid value for --exclude-internal-mappings"},
		},
		"external mapping on a reserved domain is refused": {
			args:             []string{"sync", "github", "-f", cliMapping("reserved.yaml")},
			expectedExitCode: 1,
			expectedStderr:   []string{"reserved domain mia-platform.eu", "publish it to your own domain instead"},
		},
		"external mapping named like an internal one on a reserved domain suggests the include flag": {
			args:             []string{"sync", "github", "-f", cliMapping("reserved-internal-name.yaml")},
			expectedExitCode: 1,
			expectedStderr:   []string{"reserved domain mia-platform.eu", "use --include-internal-mappings=repositories to load the internal mapping instead"},
		},
		"external mapping on a reserved subdomain is refused": {
			args:             []string{"sync", "github", "-f", cliMapping("reserved-subdomain.yaml")},
			expectedExitCode: 1,
			expectedStderr:   []string{"reserved domain mia-platform.eu", `"github.mia-platform.eu/v1"`},
		},
		"external mapping on the experimental domain is accepted": {
			args: []string{"sync", "github", "-f", cliMapping("experimental.yaml")},
			expectedLogs: [][2]string{
				{levelInfo, "github: using 0 internal mappings and 1 external mappings"},
			},
		},
		"external mapping named like a selected internal one is refused": {
			args:             []string{"sync", "github", "--include-internal-mappings=all", "-f", cliMapping("duplicate-name.yaml")},
			expectedExitCode: 1,
			expectedStderr:   []string{`duplicate mapping name "repositories"`},
		},
		"mappings sharing an item type block the start": {
			args:             []string{"sync", "github", "-f", cliMapping("shared-item-type-a.yaml"), "-f", cliMapping("shared-item-type-b.yaml")},
			expectedExitCode: 1,
			expectedStderr:   []string{"several mappings write the same item type", "--allow-shared-item-types"},
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			upstream := newFakeUpstream(t, nil)
			catalog := newFakeCatalog(t)
			env := githubEnv(upstream, catalog)
			env["HTTP_HOST"] = "127.0.0.1"
			env["HTTP_PORT"] = strconv.Itoa(freePort(t))

			result := runIBDM(t, env, test.args...)

			require.Equal(t, test.expectedExitCode, result.exitCode, "stderr:\n%s", result.stderr)
			for _, expected := range test.expectedLogs {
				assert.True(t, result.hasLog(expected[0], expected[1]), "missing %s log %q; stderr:\n%s", expected[0], expected[1], result.stderr)
			}
			for _, expected := range test.expectedStderr {
				assert.Contains(t, result.stderr, expected)
			}
			assert.Empty(t, upstream.received(), "the source must not be used")
			assert.Empty(t, catalog.received(), "nothing must reach the Catalog")
		})
	}
}

// TestMappingsCommands covers ibdm mappings list and show.
func TestMappingsCommands(t *testing.T) {
	t.Parallel()

	repositoriesMapping, err := os.ReadFile(filepath.Join("..", "mappings", "data", "github", "repositories.yaml"))
	require.NoError(t, err)

	testCases := map[string]struct {
		args             []string
		expectedExitCode int
		expectedStdout   string
		expectedStderr   string
	}{
		"list prints the internal mapping names": {
			args:           []string{"mappings", "list", "github"},
			expectedStdout: "repositories\nworkflowruns\n",
		},
		"show prints the mapping file byte for byte": {
			args:           []string{"mappings", "show", "github", "repositories"},
			expectedStdout: string(repositoriesMapping),
		},
		"list of an unknown integration is refused": {
			args:             []string{"mappings", "list", "nope"},
			expectedExitCode: 1,
			expectedStderr:   `unknown source "nope"`,
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			result := runIBDM(t, nil, test.args...)

			require.Equal(t, test.expectedExitCode, result.exitCode, "stderr:\n%s", result.stderr)
			assert.Equal(t, test.expectedStdout, result.stdout)
			assert.Contains(t, result.stderr, test.expectedStderr)
		})
	}
}

// TestRunServesReadiness proves the server harness: ibdm run with a selection starts the webhook
// server, which answers its readiness route, and nothing is fetched or sent before an event.
func TestRunServesReadiness(t *testing.T) {
	t.Parallel()

	upstream := newFakeUpstream(t, nil)
	catalog := newFakeCatalog(t)

	proc := startIBDM(t, githubEnv(upstream, catalog), "run", "github", "--include-internal-mappings=all")

	assert.Equal(t, 1, countLogs(proc.logs(), levelInfo, "github: using 2 internal mappings and 0 external mappings"))
	assert.Empty(t, upstream.received())
	assert.Empty(t, catalog.received())
}
