// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package k8s

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/mia-platform/ibdm/internal/source"
)

// helmPayload builds Helm's storage format: JSON, gzip, base64.
func helmPayload(t *testing.T, release map[string]any, compress bool) []byte {
	t.Helper()
	raw, err := json.Marshal(release)
	require.NoError(t, err)

	if compress {
		var buf bytes.Buffer
		writer := gzip.NewWriter(&buf)
		_, err = writer.Write(raw)
		require.NoError(t, err)
		require.NoError(t, writer.Close())
		raw = buf.Bytes()
	}
	return []byte(base64.StdEncoding.EncodeToString(raw))
}

func testRelease(namespace, name string, revision int) map[string]any {
	return map[string]any{
		"name":      name,
		"namespace": namespace,
		"version":   revision,
		"info": map[string]any{
			"status":         "deployed",
			"first_deployed": "2024-01-01T00:00:00Z",
			"last_deployed":  "2024-02-01T00:00:00Z",
		},
		"chart": map[string]any{
			"metadata": map[string]any{"name": "web-chart", "version": "1.2.3", "appVersion": "1.20"},
			"files":    []any{map[string]any{"name": "ignored", "data": "c2VjcmV0"}},
		},
		"manifest": "kind: Secret",
		"config":   map[string]any{"password": "ignored"},
	}
}

func newHelmSecret(namespace, name string, revision int, status string, data []byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "sh.helm.release.v1." + name + ".v" + strconv.Itoa(revision),
			Labels: map[string]string{
				"owner":   "helm",
				"name":    name,
				"status":  status,
				"version": strconv.Itoa(revision),
			},
		},
		Data: map[string][]byte{"release": data},
	}
}

func syncHelm(t *testing.T, s *Source) []source.Data {
	t.Helper()
	results := make(chan source.Data, 20)
	require.NoError(t, s.syncHelmReleases(t.Context(), results))
	close(results)
	return collectData(results)
}

func TestSyncHelmReleases(t *testing.T) {
	setupFixedTime(t)

	s := newFakeSource(t,
		newHelmSecret("team-a", "web", 3, "deployed", helmPayload(t, testRelease("team-a", "web", 3), true)),
		newHelmSecret("team-a", "web", 2, "superseded", helmPayload(t, testRelease("team-a", "web", 2), true)),
		newHelmSecret("team-b", "api", 1, "deployed", helmPayload(t, testRelease("team-b", "api", 1), false)),
		newHelmSecret("team-b", "broken", 1, "failed", helmPayload(t, testRelease("team-b", "broken", 1), true)),
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "other"}, Data: map[string][]byte{"release": []byte("x")}},
	)

	items := syncHelm(t, s)
	require.Len(t, items, 2)

	assert.Equal(t, helmReleaseType, items[0].Type)
	assert.Equal(t, source.DataOperationUpsert, items[0].Operation)
	assert.Equal(t, testFixedTime, items[0].Time)
	assert.Equal(t, map[string]any{
		"apiServer":     testAPIServer,
		"name":          "web",
		"namespace":     "team-a",
		"revision":      3,
		"status":        "deployed",
		"chartName":     "web-chart",
		"chartVersion":  "1.2.3",
		"appVersion":    "1.20",
		"firstDeployed": "2024-01-01T00:00:00Z",
		"lastDeployed":  "2024-02-01T00:00:00Z",
	}, items[0].Values)
	assert.Equal(t, "api", items[1].Values["name"])
	assert.Equal(t, "team-b", items[1].Values["namespace"])
}

func TestSyncHelmReleasesEmpty(t *testing.T) {
	t.Parallel()
	assert.Empty(t, syncHelm(t, newFakeSource(t)))
}

func TestSyncHelmReleasesMissingChartFields(t *testing.T) {
	t.Parallel()

	release := map[string]any{"name": "web", "namespace": "team-a", "version": 1}
	s := newFakeSource(t, newHelmSecret("team-a", "web", 1, "deployed", helmPayload(t, release, true)))

	items := syncHelm(t, s)
	require.Len(t, items, 1)
	values := items[0].Values
	for _, key := range []string{"status", "chartName", "chartVersion", "appVersion", "firstDeployed", "lastDeployed"} {
		assert.Empty(t, values[key], key)
	}
	assert.Equal(t, 1, values["revision"])
}

func TestSyncHelmReleasesNonStringDates(t *testing.T) {
	t.Parallel()

	release := testRelease("team-a", "web", 1)
	info, ok := release["info"].(map[string]any)
	require.True(t, ok)
	info["first_deployed"] = map[string]any{"seconds": 1}
	info["last_deployed"] = nil
	s := newFakeSource(t, newHelmSecret("team-a", "web", 1, "deployed", helmPayload(t, release, true)))

	items := syncHelm(t, s)
	require.Len(t, items, 1)
	assert.Empty(t, items[0].Values["firstDeployed"])
	assert.Empty(t, items[0].Values["lastDeployed"])
	assert.Equal(t, "deployed", items[0].Values["status"])
}

func TestSyncHelmReleasesCorruptSecretsSkipped(t *testing.T) {
	t.Parallel()

	goodGzip := helmPayload(t, testRelease("team-a", "web", 1), true)
	badGzip := base64.StdEncoding.EncodeToString(append([]byte{0x1f, 0x8b}, []byte("not gzip")...))

	s := newFakeSource(t,
		newHelmSecret("team-a", "web", 1, "deployed", goodGzip),
		newHelmSecret("team-a", "bad-base64", 1, "deployed", []byte("%%% not base64")),
		newHelmSecret("team-a", "bad-gzip", 1, "deployed", []byte(badGzip)),
		newHelmSecret("team-a", "bad-json", 1, "deployed", []byte(base64.StdEncoding.EncodeToString([]byte("{not json")))),
		newHelmSecret("team-a", "no-data", 1, "deployed", nil),
	)

	items := syncHelm(t, s)
	require.Len(t, items, 1)
	assert.Equal(t, "web", items[0].Values["name"])
}

func TestSyncHelmReleasesDuplicateHighestRevision(t *testing.T) {
	t.Parallel()

	low := testRelease("team-a", "web", 2)
	high := testRelease("team-a", "web", 5)
	high["chart"] = map[string]any{"metadata": map[string]any{"name": "web-chart", "version": "9.9.9"}}

	s := newFakeSource(t,
		newHelmSecret("team-a", "web", 5, "deployed", helmPayload(t, high, true)),
		newHelmSecret("team-a", "web-copy", 2, "deployed", helmPayload(t, low, true)),
	)

	items := syncHelm(t, s)
	require.Len(t, items, 1)
	assert.Equal(t, 5, items[0].Values["revision"])
	assert.Equal(t, "9.9.9", items[0].Values["chartVersion"])
}

func TestSyncHelmReleasesFallbacks(t *testing.T) {
	t.Parallel()

	s := newFakeSource(t, newHelmSecret("team-a", "web", 4, "deployed", helmPayload(t, map[string]any{}, true)))

	items := syncHelm(t, s)
	require.Len(t, items, 1)
	assert.Equal(t, "web", items[0].Values["name"])
	assert.Equal(t, "team-a", items[0].Values["namespace"])
	assert.Equal(t, 4, items[0].Values["revision"])
}

func TestSyncHelmReleasesPagination(t *testing.T) {
	setupFixedTime(t)

	var selectors, limits []string
	payload := helmPayload(t, testRelease("team-a", "web", 1), true)
	s := newHTTPSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/secrets", r.URL.Path)
		selectors = append(selectors, r.URL.Query().Get("labelSelector"))
		limits = append(limits, r.URL.Query().Get("limit"))
		if r.URL.Query().Get("continue") == "" {
			writeJSON(t, w, corev1.SecretList{
				ListMeta: metav1.ListMeta{Continue: "page-2"},
				Items:    []corev1.Secret{*newHelmSecret("team-a", "web", 1, "deployed", payload)},
			})
			return
		}
		assert.Equal(t, "page-2", r.URL.Query().Get("continue"))
		writeJSON(t, w, corev1.SecretList{Items: []corev1.Secret{
			*newHelmSecret("team-b", "api", 1, "deployed", helmPayload(t, testRelease("team-b", "api", 1), true)),
		}})
	}))

	results := make(chan source.Data, 5)
	require.NoError(t, s.syncHelmReleases(t.Context(), results))
	close(results)

	assert.Len(t, collectData(results), 2)
	assert.Equal(t, []string{"500", "500"}, limits)
	assert.Equal(t, []string{helmSecretSelector, helmSecretSelector}, selectors)
}

func TestSyncHelmReleasesListError(t *testing.T) {
	t.Parallel()

	s := newHTTPSource(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	require.ErrorIs(t, s.syncHelmReleases(t.Context(), make(chan source.Data, 1)), ErrRetrievingAssets)
}

func TestSyncHelmReleasesContextCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	s := newFakeSource(t, newHelmSecret("team-a", "web", 1, "deployed", helmPayload(t, testRelease("team-a", "web", 1), true)))
	require.ErrorIs(t, s.syncHelmReleases(ctx, make(chan source.Data)), context.Canceled)
}

func TestSyncHelmReleasesCanceledWhileSending(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	s := newFakeSource(t, newHelmSecret("team-a", "web", 1, "deployed", helmPayload(t, testRelease("team-a", "web", 1), true)))

	done := make(chan error, 1)
	go func() { done <- s.syncHelmReleases(ctx, make(chan source.Data)) }()
	cancel()

	require.ErrorIs(t, <-done, context.Canceled)
}

func TestStartSyncProcessHelmReleaseDispatchOrder(t *testing.T) {
	setupFixedTime(t)

	s := newFakeSource(t,
		newNamespace("team-a", nil),
		newService("team-a", "web", nil, corev1.ServiceSpec{}),
		newHelmSecret("team-a", "web", 1, "deployed", helmPayload(t, testRelease("team-a", "web", 1), true)),
	)
	results := make(chan source.Data, 10)

	types := map[string]source.Extra{helmReleaseType: {}, serviceType: {}, namespaceType: {}, "unknown": {}}
	require.NoError(t, s.StartSyncProcess(t.Context(), types, results))
	close(results)

	collected := collectData(results)
	got := make([]string, 0, len(collected))
	for _, item := range collected {
		got = append(got, item.Type)
	}
	assert.Equal(t, []string{namespaceType, serviceType, helmReleaseType}, got)
}

func TestStartSyncProcessHelmReleaseErrorIsolation(t *testing.T) {
	setupFixedTime(t)

	s := newHTTPSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/namespaces" {
			writeJSON(t, w, corev1.NamespaceList{Items: []corev1.Namespace{*newNamespace("team-a", nil)}})
			return
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	results := make(chan source.Data, 10)

	err := s.StartSyncProcess(t.Context(), map[string]source.Extra{namespaceType: {}, helmReleaseType: {}}, results)
	require.ErrorIs(t, err, ErrRetrievingAssets)
	close(results)

	items := collectData(results)
	require.Len(t, items, 1)
	assert.Equal(t, namespaceType, items[0].Type)
}
