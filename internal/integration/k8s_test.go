// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

//go:build integration

package integration

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	k8sUpstreamDir = "testdata/upstream/k8s"

	// k8sAPIServer is the fictional address of the cluster. Every k8s identifier hashes it, so it
	// must not change between runs: the client reaches the fake through HTTP_PROXY instead.
	k8sAPIServer = "http://k8s-api.example.com"

	k8sAPIVersion = "k8s.mia-platform.eu/v1"
	// k8sConsoleGroup is written by the Console integration: the cluster item points at its Cluster.
	k8sConsoleGroup = "console.mia-platform.eu"

	k8sPodsPath            = "/api/v1/pods"
	k8sServicesPath        = "/api/v1/services"
	k8sDeploymentsPath     = "/apis/apps/v1/deployments"
	k8sStatefulSetsPath    = "/apis/apps/v1/statefulsets"
	k8sDaemonSetsPath      = "/apis/apps/v1/daemonsets"
	k8sNetworkPoliciesPath = "/apis/networking.k8s.io/v1/networkpolicies"
	k8sIngressRoutesPath   = "/apis/traefik.io/v1alpha1/ingressroutes"

	k8sPodURNPrefix         = "urn:mia-platform-catalog:k8s.mia-platform.eu:v1:K8sPod:"
	k8sDeploymentURNPrefix  = "urn:mia-platform-catalog:k8s.mia-platform.eu:v1:K8sDeployment:"
	k8sStatefulSetURNPrefix = "urn:mia-platform-catalog:k8s.mia-platform.eu:v1:K8sStatefulSet:"
)

// k8sEnv points the k8s source at the fictional API server, reached through upstream acting as
// an HTTP proxy, and the Catalog destination at catalog. The Catalog, on a loopback address, is
// never proxied; NO_PROXY makes it explicit.
func k8sEnv(upstream *fakeUpstream, catalog *fakeCatalog) map[string]string {
	env := catalog.env()
	env["HTTP_PROXY"] = upstream.baseURL()
	env["NO_PROXY"] = "127.0.0.1,localhost"
	env["K8S_API_SERVER"] = k8sAPIServer
	env["K8S_BEARER_TOKEN"] = "test-token"
	return env
}

// k8sRoutes returns overrides followed by the fixture routes: the first matching route wins, so an
// override replaces the fixture answer of its path.
func k8sRoutes(t *testing.T, overrides ...route) []route {
	t.Helper()

	return append(overrides, loadRoutes(t, k8sUpstreamDir)...)
}

// k8sRoute builds a route answering path with status and a fixture body of the k8s directory.
func k8sRoute(t *testing.T, path string, status int, bodyFile string) route {
	t.Helper()

	body, err := os.ReadFile(filepath.Join(k8sUpstreamDir, bodyFile))
	require.NoError(t, err)
	return route{Method: http.MethodGet, Path: path, Status: status, body: body}
}

// k8sRelationshipEnds counts the relationship ends of items whose URN starts with prefix.
func k8sRelationshipEnds(t *testing.T, items []map[string]any, end, prefix string) int {
	t.Helper()

	count := 0
	for _, relationship := range itemsOf(items, relationshipsAPIVersion, "relationships") {
		spec, ok := relationship["data"].(map[string]any)
		require.True(t, ok)
		if ref, ok := spec[end].(string); ok && strings.HasPrefix(ref, prefix) {
			count++
		}
	}
	return count
}

// TestK8sSync covers ibdm sync k8s end to end, and checks the claims of the "Internal Mappings
// and Their Dependencies" and "Sync" sections of docs/how-to/130_k8s-source.md. ibdm run k8s is
// out of scope (plan U-4).
func TestK8sSync(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		args             []string
		overrides        func(t *testing.T) []route
		expectedExitCode int
		golden           string
		check            func(t *testing.T, run result, items []map[string]any, upstream *fakeUpstream)
	}{
		"K1 all internal mappings": {
			args:   []string{"--include-internal-mappings=all"},
			golden: "k8s/all",
			check: func(t *testing.T, run result, items []map[string]any, upstream *fakeUpstream) {
				t.Helper()
				assert.Zero(t, countLogsAtLevel(run.logs, levelError), "stderr:\n%s", run.stderr)
				assert.Equal(t, map[string]int{
					k8sAPIVersion + " k8sclusters":             1,
					k8sAPIVersion + " k8snamespaces":           2,
					k8sAPIVersion + " k8sdeployments":          1,
					k8sAPIVersion + " k8sstatefulsets":         1,
					k8sAPIVersion + " k8sdaemonsets":           1,
					k8sAPIVersion + " k8sservices":             3,
					k8sAPIVersion + " k8snetworkpolicies":      1,
					k8sAPIVersion + " k8shelmreleases":         1,
					k8sAPIVersion + " traefikingressroutes":    1,
					k8sAPIVersion + " k8scertificates":         1,
					k8sAPIVersion + " k8spods":                 3,
					relationshipsAPIVersion + " relationships": 16 + 1 + 1 + 2 + 2,
				}, countItems(items), "16 resource items, one relationship each from their mappings, plus "+
					"1 route to service (none to api@internal), 1 workload to release, 2 services to workloads "+
					"(none for the ExternalName), 2 pods to owners (none for the Job pod); the Succeeded pod is not synchronised")
				assertRelationshipsResolve(t, items, k8sConsoleGroup)
				assert.Equal(t, 2, upstream.calls(http.MethodGet, "/api/v1/namespaces"), "both namespace pages, through the continue token")
			},
		},
		"K2 a relationship mapping alone lists its objects without writing them": {
			args:   []string{"--include-internal-mappings=pod-owner-relationships"},
			golden: "k8s/pod-owner-relationships-only",
			check: func(t *testing.T, _ result, items []map[string]any, upstream *fakeUpstream) {
				t.Helper()
				assert.Equal(t, map[string]int{relationshipsAPIVersion + " relationships": 2}, countItems(items))
				assert.Equal(t, 1, upstream.calls(http.MethodGet, k8sPodsPath))
				assert.Equal(t, 2, k8sRelationshipEnds(t, items, "sourceRef", k8sPodURNPrefix), "from pod items nothing creates")
				assert.Equal(t, 1, k8sRelationshipEnds(t, items, "targetRef", k8sDeploymentURNPrefix))
				assert.Equal(t, 1, k8sRelationshipEnds(t, items, "targetRef", k8sStatefulSetURNPrefix))
			},
		},
		"K3 a resource and a relationship derived from it list the objects twice": {
			args:   []string{"--include-internal-mappings=pods,pod-owner-relationships"},
			golden: "k8s/pods-and-owners",
			check: func(t *testing.T, _ result, items []map[string]any, upstream *fakeUpstream) {
				t.Helper()
				assert.Equal(t, 2, upstream.calls(http.MethodGet, k8sPodsPath))
				assert.Len(t, itemsOf(items, k8sAPIVersion, "k8spods"), 3)
			},
		},
		"K4 service to workload links follow the selectors": {
			args:   []string{"--include-internal-mappings=service-workload-relationships"},
			golden: "k8s/service-workload-relationships-only",
			check: func(t *testing.T, _ result, items []map[string]any, upstream *fakeUpstream) {
				t.Helper()
				for _, path := range []string{k8sServicesPath, k8sDeploymentsPath, k8sStatefulSetsPath, k8sDaemonSetsPath} {
					assert.Equal(t, 1, upstream.calls(http.MethodGet, path), "lists %s", path)
				}
				assert.Equal(t, map[string]int{relationshipsAPIVersion + " relationships": 2}, countItems(items))
				assert.Equal(t, 1, k8sRelationshipEnds(t, items, "targetRef", k8sDeploymentURNPrefix), "catalog-api selects the Deployment")
				assert.Equal(t, 1, k8sRelationshipEnds(t, items, "targetRef", k8sStatefulSetURNPrefix), "orders-db selects the StatefulSet")
			},
		},
		"K5 a custom resource that is not installed is skipped": {
			args: []string{"--include-internal-mappings=ingressroutes,certificates"},
			overrides: func(t *testing.T) []route {
				t.Helper()
				return []route{k8sRoute(t, "/apis", http.StatusOK, "api-groups-without-traefik.json")}
			},
			golden: "k8s/without-traefik",
			check: func(t *testing.T, run result, items []map[string]any, upstream *fakeUpstream) {
				t.Helper()
				assert.Zero(t, countLogsAtLevel(run.logs, levelError), "stderr:\n%s", run.stderr)
				assert.Equal(t, 1, countLogs(run.logs, levelInfo, "CRD not installed, skipping type"))
				assert.Empty(t, itemsOf(items, k8sAPIVersion, "traefikingressroutes"))
				assert.Len(t, itemsOf(items, k8sAPIVersion, "k8scertificates"), 1)
				assert.Zero(t, upstream.calls(http.MethodGet, k8sIngressRoutesPath))
			},
		},
		"K6 a failing type is logged, the others are written, and the sync fails at the end": {
			args: []string{"--include-internal-mappings=all"},
			overrides: func(t *testing.T) []route {
				t.Helper()
				return []route{k8sRoute(t, k8sNetworkPoliciesPath, http.StatusInternalServerError, "status-error.json")}
			},
			expectedExitCode: 1,
			golden:           "k8s/networkpolicies-failure",
			check: func(t *testing.T, run result, items []map[string]any, _ *fakeUpstream) {
				t.Helper()
				assert.Equal(t, 1, countLogs(run.logs, levelError, "error syncing type"))
				assert.Empty(t, itemsOf(items, k8sAPIVersion, "k8snetworkpolicies"))
				assert.Len(t, items, 38-2, "everything but the network policy and its namespace relationship")
			},
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var overrides []route
			if test.overrides != nil {
				overrides = test.overrides(t)
			}
			upstream := newFakeUpstream(t, k8sRoutes(t, overrides...), "Authorization")
			catalog := newFakeCatalog(t)

			run := runIBDM(t, k8sEnv(upstream, catalog), append([]string{"sync", "k8s"}, test.args...)...)
			require.Equal(t, test.expectedExitCode, run.exitCode, "stderr:\n%s", run.stderr)

			test.check(t, run, catalog.received(), upstream)
			assertGolden(t, test.golden, catalog, upstream)
		})
	}
}
