// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

//go:build integration

package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// baselineBinEnv names the ibdm binary of the previous release.
	baselineBinEnv = "IBDM_BASELINE_BIN"
	// baselineMappingsEnv names the docs/mappings directory of the previous release.
	baselineMappingsEnv = "IBDM_BASELINE_MAPPINGS"
)

// baselineCase is the full-selection sync of one integration, run by both binaries.
type baselineCase struct {
	// slug is the integration name on the command line and the mapping directory.
	slug        string
	upstreamDir string
	env         func(t *testing.T, upstream *fakeUpstream, catalog *fakeCatalog) map[string]string
}

// baselineCases lists the eight integrations the fakes can serve, with the fixtures and the
// environment of their full-selection sync scenario.
var baselineCases = []baselineCase{
	{slug: "azure-devops", upstreamDir: azuredevopsUpstreamDir, env: withoutT(azuredevopsEnv)},
	{slug: "bitbucket", upstreamDir: bitbucketUpstreamDir, env: withoutT(bitbucketEnv)},
	{slug: "console", upstreamDir: consoleUpstreamDir, env: withoutT(consoleEnv)},
	{slug: "github", upstreamDir: githubUpstreamDir, env: withoutT(githubEnv)},
	{slug: "gitlab", upstreamDir: gitlabUpstreamDir, env: withoutT(gitlabEnv)},
	{slug: "nexus", upstreamDir: nexusUpstreamDir, env: func(t *testing.T, upstream *fakeUpstream, catalog *fakeCatalog) map[string]string {
		t.Helper()
		return nexusEnv(t, upstream, catalog, "")
	}},
	{slug: "sonarqube", upstreamDir: sonarqubeUpstreamDir, env: withoutT(sonarqubeEnv)},
	{slug: "sysdig", upstreamDir: sysdigPagedUpstreamDir, env: func(_ *testing.T, upstream *fakeUpstream, catalog *fakeCatalog) map[string]string {
		env := sysdigEnv(upstream, catalog)
		env["SYSDIG_PAGE_SIZE"] = "2"
		return env
	}},
}

// withoutT adapts an environment helper that needs no *testing.T.
func withoutT(env func(*fakeUpstream, *fakeCatalog) map[string]string) func(*testing.T, *fakeUpstream, *fakeCatalog) map[string]string {
	return func(_ *testing.T, upstream *fakeUpstream, catalog *fakeCatalog) map[string]string {
		return env(upstream, catalog)
	}
}

// TestBaseline compares this branch with the previous release: for every integration the fakes
// can serve, the previous binary with the mapping files it shipped (--mapping-file
// docs/mappings/<integration>) and this binary with its internal mappings
// (--include-internal-mappings=all) must send the same items to the Catalog and make the same
// upstream requests.
//
// It is skipped unless IBDM_BASELINE_BIN and IBDM_BASELINE_MAPPINGS are set. To prepare them
// without touching the git state of the repository:
//
//	git fetch origin
//	mkdir -p .tmp/baseline
//	git archive origin/main | tar -x -C .tmp/baseline
//	(cd .tmp/baseline && go build -o ibdm .)
//	IBDM_BASELINE_BIN=$PWD/.tmp/baseline/ibdm IBDM_BASELINE_MAPPINGS=$PWD/.tmp/baseline/docs/mappings \
//	  go test -tags=integration -race -count=1 -run TestBaseline ./internal/integration/...
func TestBaseline(t *testing.T) {
	t.Parallel()

	baselineBinary := os.Getenv(baselineBinEnv)
	baselineMappings := os.Getenv(baselineMappingsEnv)
	if baselineBinary == "" || baselineMappings == "" {
		t.Skipf("set %s and %s to compare with a previous release", baselineBinEnv, baselineMappingsEnv)
	}

	for _, test := range baselineCases {
		t.Run(test.slug, func(t *testing.T) {
			t.Parallel()

			mappingDir := filepath.Join(baselineMappings, test.slug)
			_, err := os.Stat(mappingDir)
			require.NoError(t, err, "the previous release has no mappings for %s", test.slug)

			// One upstream for both runs: identifiers that hash its address, such as those of
			// Nexus, are then the same in both.
			upstream := newFakeUpstream(t, loadRoutes(t, test.upstreamDir))
			baselineCatalog := newFakeCatalog(t)
			currentCatalog := newFakeCatalog(t)

			baseline := runBinary(t, baselineBinary, test.env(t, upstream, baselineCatalog), "sync", test.slug, "--mapping-file", mappingDir)
			require.Equal(t, 0, baseline.exitCode, "previous release; stderr:\n%s", baseline.stderr)
			baselineRequests := len(upstream.received())

			current := runIBDM(t, test.env(t, upstream, currentCatalog), "sync", test.slug, "--include-internal-mappings=all")
			require.Equal(t, 0, current.exitCode, "this branch; stderr:\n%s", current.stderr)

			replacer := strings.NewReplacer(
				hostOf(t, baselineCatalog.server.URL), catalogPlaceholder,
				hostOf(t, currentCatalog.server.URL), catalogPlaceholder,
				hostOf(t, upstream.server.URL), upstreamPlaceholder,
			)
			requests := upstream.received()
			expected := normalise(golden{CatalogItems: baselineCatalog.received(), UpstreamRequests: requests[:baselineRequests]}, replacer)
			actual := normalise(golden{CatalogItems: currentCatalog.received(), UpstreamRequests: requests[baselineRequests:]}, replacer)

			require.NotEmpty(t, expected.CatalogItems, "the previous release wrote nothing: the comparison would prove nothing")
			assert.Equal(t, expected.CatalogItems, actual.CatalogItems, "items sent to the Catalog")
			assert.Equal(t, expected.UpstreamRequests, actual.UpstreamRequests, "requests to the upstream")
		})
	}
}
