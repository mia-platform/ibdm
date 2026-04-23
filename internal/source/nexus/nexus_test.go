// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package nexus

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/source"
)

var testTime = time.Date(2025, time.March, 1, 12, 0, 0, 0, time.UTC)

func init() {
	timeSource = func() time.Time {
		return testTime
	}
}

func newTestSource(t *testing.T, handler http.Handler, specificRepository string) *Source {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	u, err := url.Parse(server.URL)
	require.NoError(t, err)

	return &Source{
		config: config{
			URLSchema:          u.Scheme,
			URLHost:            u.Host,
			TokenName:          "test-token",
			TokenPasscode:      "test-passcode",
			HTTPTimeout:        5 * time.Second,
			SpecificRepository: specificRepository,
		},
		webhookConfig: webhookConfig{
			WebhookPath: "/nexus/webhook",
		},
		client: &client{
			baseURL:       u,
			tokenName:     "test-token",
			tokenPasscode: "test-passcode",
			httpClient: &http.Client{
				Timeout: 5 * time.Second,
			},
		},
	}
}

func collectData(t *testing.T, ch <-chan source.Data) []source.Data {
	t.Helper()
	var result []source.Data
	for d := range ch {
		result = append(result, d)
	}
	return result
}

func TestNewSource(t *testing.T) {
	testCases := map[string]struct {
		setupEnv    func(t *testing.T)
		expectErr   bool
		expectErrIs error
	}{
		"valid configuration": {
			setupEnv: func(t *testing.T) {
				t.Helper()
				t.Setenv("NEXUS_URL_SCHEMA", "https")
				t.Setenv("NEXUS_URL_HOST", "nexus.example.com")
				t.Setenv("NEXUS_TOKEN_NAME", "mytoken")
				t.Setenv("NEXUS_TOKEN_PASSCODE", "secret")
			},
		},
		"missing required env vars": {
			setupEnv: func(t *testing.T) {
				t.Helper()
			},
			expectErr:   true,
			expectErrIs: ErrNexusSource,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			tc.setupEnv(t)
			s, err := NewSource()
			if tc.expectErr {
				require.Error(t, err)
				if tc.expectErrIs != nil {
					require.ErrorIs(t, err, tc.expectErrIs)
				}
				return
			}
			require.NoError(t, err)
			require.NotNil(t, s)
		})
	}
}

func TestStartSyncProcess(t *testing.T) {
	t.Parallel()

	sampleRepos := []map[string]any{
		{"name": "maven-central", "format": "maven2", "type": "proxy", "url": "https://nexus.example.com/repository/maven-central", "attributes": map[string]any{}},
		{"name": "docker-hosted", "format": "docker", "type": "hosted", "url": "https://nexus.example.com/repository/docker-hosted", "attributes": map[string]any{}},
	}

	sampleComponents := componentsPageResponse{
		Items: []map[string]any{
			{
				"id":         "comp1",
				"repository": "docker-hosted",
				"format":     "docker",
				"name":       "my-image",
				"version":    "1.0.0",
				"assets": []any{
					map[string]any{
						"downloadUrl": "https://nexus.example.com/repository/docker-hosted/v2/my-image/manifests/1.0.0",
						"path":        "v2/my-image/manifests/1.0.0",
						"id":          "asset1",
						"repository":  "docker-hosted",
						"format":      "docker",
						"checksum":    map[string]any{"sha1": "abc123", "sha256": "def456"},
						"contentType": "application/vnd.docker.distribution.manifest.v2+json",
						"fileSize":    float64(1234),
					},
					map[string]any{
						"downloadUrl": "https://nexus.example.com/repository/docker-hosted/v2/my-image/blobs/sha256:abc",
						"path":        "v2/my-image/blobs/sha256:abc",
						"id":          "asset2",
						"repository":  "docker-hosted",
						"format":      "docker",
						"checksum":    map[string]any{"sha1": "xyz789", "sha256": "uvw012"},
						"contentType": "application/vnd.docker.distribution.layer.v1.tar+gzip",
						"fileSize":    float64(654321),
					},
				},
				"tags": []any{},
			},
		},
		ContinuationToken: nil,
	}

	standardMux := func() *http.ServeMux {
		mux := http.NewServeMux()
		mux.HandleFunc("/service/rest/v1/repositories", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sampleRepos)
		})
		mux.HandleFunc("/service/rest/v1/components", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			repo := r.URL.Query().Get("repository")
			if repo == "docker-hosted" {
				_ = json.NewEncoder(w).Encode(sampleComponents)
			} else {
				_ = json.NewEncoder(w).Encode(componentsPageResponse{Items: []map[string]any{}, ContinuationToken: nil})
			}
		})
		return mux
	}

	testCases := map[string]struct {
		handler            http.Handler
		specificRepository string
		typesToSync        map[string]source.Extra
		expectedDataCount  int
		validateData       func(t *testing.T, data []source.Data)
	}{
		"sync component assets from all repos": {
			handler:     standardMux(),
			typesToSync: map[string]source.Extra{componentAssetType: {}},
			// docker-hosted: 1 dockerImageType + 2 componentAssetType; maven-central: 0 (non-docker skipped)
			expectedDataCount: 3,
			validateData: func(t *testing.T, data []source.Data) {
				t.Helper()
				dockerImageCount := 0
				componentAssetCount := 0
				for _, d := range data {
					assert.Equal(t, source.DataOperationUpsert, d.Operation)
					assert.Equal(t, testTime, d.Time)
					assert.NotEmpty(t, d.Values["host"])
					switch d.Type {
					case dockerImageType:
						dockerImageCount++
						assert.Equal(t, "my-image", d.Values["name"])
						assert.Equal(t, "1.0.0", d.Values["version"])
					case componentAssetType:
						componentAssetCount++
						assert.NotNil(t, d.Values["asset"])
						assert.Nil(t, d.Values["assets"])
						assert.Equal(t, "my-image", d.Values["name"])
					}
				}
				assert.Equal(t, 1, dockerImageCount)
				assert.Equal(t, 2, componentAssetCount)
			},
		},
		"sync with specific repository": {
			handler: func() http.Handler {
				mux := http.NewServeMux()
				mux.HandleFunc("/service/rest/v1/repositories/docker-hosted", func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(sampleRepos[1])
				})
				mux.HandleFunc("/service/rest/v1/components", func(w http.ResponseWriter, r *http.Request) {
					require.Equal(t, "docker-hosted", r.URL.Query().Get("repository"))
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(sampleComponents)
				})
				// List all repos should NOT be called.
				mux.HandleFunc("/service/rest/v1/repositories", func(_ http.ResponseWriter, _ *http.Request) {
					t.Fatal("list all repositories should not be called when specific repo is set")
				})
				return mux
			}(),
			specificRepository: "docker-hosted",
			typesToSync:        map[string]source.Extra{componentAssetType: {}},
			// 1 dockerImageType + 2 componentAssetType
			expectedDataCount: 3,
			validateData: func(t *testing.T, data []source.Data) {
				t.Helper()
				for _, d := range data {
					assert.Contains(t, []string{dockerImageType, componentAssetType}, d.Type)
					assert.NotEmpty(t, d.Values["host"])
				}
			},
		},
		"unknown type is skipped": {
			handler:           standardMux(),
			typesToSync:       map[string]source.Extra{"unknown-type": {}},
			expectedDataCount: 0,
		},
		"empty typesToSync": {
			handler: func() http.Handler {
				return http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
					t.Fatal("no API calls should be made when typesToSync is empty")
				})
			}(),
			typesToSync:       map[string]source.Extra{},
			expectedDataCount: 0,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := newTestSource(t, tc.handler, tc.specificRepository)

			ch := make(chan source.Data, 100)
			var data []source.Data

			done := make(chan struct{})
			go func() {
				defer close(done)
				data = collectData(t, ch)
			}()

			err := s.StartSyncProcess(t.Context(), tc.typesToSync, ch)
			close(ch)
			<-done

			require.NoError(t, err)
			assert.Len(t, data, tc.expectedDataCount)
			if tc.validateData != nil {
				tc.validateData(t, data)
			}
		})
	}
}

func TestFanOut(t *testing.T) {
	t.Parallel()

	componentWith3Assets := componentsPageResponse{
		Items: []map[string]any{
			{
				"id":         "comp1",
				"repository": "docker-hosted",
				"format":     "docker",
				"name":       "my-image",
				"version":    "2.0.0",
				"assets": []any{
					map[string]any{"id": "a1", "path": "v2/my-image/manifests/2.0.0", "checksum": map[string]any{"sha256": "hash1"}},
					map[string]any{"id": "a2", "path": "v2/my-image/blobs/sha256:aaa", "checksum": map[string]any{"sha256": "hash2"}},
					map[string]any{"id": "a3", "path": "v2/my-image/blobs/sha256:bbb", "checksum": map[string]any{"sha256": "hash3"}},
				},
				"tags": []any{},
			},
		},
		ContinuationToken: nil,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/service/rest/v1/repositories", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"name": "docker-hosted", "format": "docker", "type": "hosted"},
		})
	})
	mux.HandleFunc("/service/rest/v1/components", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(componentWith3Assets)
	})

	s := newTestSource(t, mux, "")
	ch := make(chan source.Data, 100)
	var data []source.Data

	done := make(chan struct{})
	go func() {
		defer close(done)
		data = collectData(t, ch)
	}()

	err := s.StartSyncProcess(t.Context(), map[string]source.Extra{componentAssetType: {}}, ch)
	close(ch)
	<-done

	require.NoError(t, err)
	// 1 dockerImageType + 3 componentAssetType
	require.Len(t, data, 4)

	// First entry is the dockerImageType for the component.
	assert.Equal(t, dockerImageType, data[0].Type)
	assert.Equal(t, "my-image", data[0].Values["name"])
	assert.Equal(t, "2.0.0", data[0].Values["version"])
	assert.Equal(t, s.config.URLHost, data[0].Values["host"])

	// Remaining entries are componentAssetType, one per asset.
	expectedAssets := []struct{ id, path string }{
		{"a1", "v2/my-image/manifests/2.0.0"},
		{"a2", "v2/my-image/blobs/sha256:aaa"},
		{"a3", "v2/my-image/blobs/sha256:bbb"},
	}
	for i, expected := range expectedAssets {
		d := data[i+1]
		assert.Equal(t, componentAssetType, d.Type)
		assert.Equal(t, "my-image", d.Values["name"])
		assert.Equal(t, "2.0.0", d.Values["version"])
		assert.Equal(t, "docker", d.Values["format"])
		assert.Equal(t, s.config.URLHost, d.Values["host"])
		assert.Nil(t, d.Values["assets"], "original assets array must not be in the flattened map")

		asset, ok := d.Values["asset"].(map[string]any)
		require.True(t, ok, "asset must be a map")
		assert.Equal(t, expected.id, asset["id"])
		assert.Equal(t, expected.path, asset["path"])
	}
}

func TestZeroAssetsSkipped(t *testing.T) {
	t.Parallel()

	componentWithNoAssets := componentsPageResponse{
		Items: []map[string]any{
			{
				"id":         "comp1",
				"repository": "docker-hosted",
				"format":     "docker",
				"name":       "empty-image",
				"version":    "1.0.0",
				"assets":     []any{},
				"tags":       []any{},
			},
		},
		ContinuationToken: nil,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/service/rest/v1/repositories", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"name": "docker-hosted", "format": "docker", "type": "hosted"},
		})
	})
	mux.HandleFunc("/service/rest/v1/components", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(componentWithNoAssets)
	})

	s := newTestSource(t, mux, "")
	ch := make(chan source.Data, 100)
	var data []source.Data

	done := make(chan struct{})
	go func() {
		defer close(done)
		data = collectData(t, ch)
	}()

	err := s.StartSyncProcess(t.Context(), map[string]source.Extra{componentAssetType: {}}, ch)
	close(ch)
	<-done

	require.NoError(t, err)
	assert.Empty(t, data)
}

func TestConcurrencyGuard(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/service/rest/v1/repositories", func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("no API calls should be made when the sync lock is already held")
	})

	s := newTestSource(t, mux, "")

	// Simulate an in-flight sync by holding the lock.
	s.syncLock.Lock()

	ch := make(chan source.Data, 100)
	err := s.StartSyncProcess(t.Context(), map[string]source.Extra{componentAssetType: {}}, ch)
	close(ch)

	assert.NoError(t, err)
	assert.Empty(t, collectData(t, ch))

	s.syncLock.Unlock()
}

func TestContextCancellationInSync(t *testing.T) {
	t.Parallel()

	callCount := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/service/rest/v1/repositories", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"name": "docker-hosted", "format": "docker", "type": "hosted"},
		})
	})
	mux.HandleFunc("/service/rest/v1/components", func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		token := "next"
		_ = json.NewEncoder(w).Encode(componentsPageResponse{
			Items: []map[string]any{
				{
					"id": "comp1", "repository": "docker-hosted", "format": "docker",
					"name": "my-image", "version": "1.0",
					"assets": []any{map[string]any{"id": "a1", "path": "v2/my-image/manifests/1.0"}},
					"tags":   []any{},
				},
			},
			ContinuationToken: &token,
		})
	})

	s := newTestSource(t, mux, "")
	ctx, cancel := context.WithCancel(t.Context())

	ch := make(chan source.Data, 100)
	done := make(chan error, 1)
	go func() {
		done <- s.StartSyncProcess(ctx, map[string]source.Extra{componentAssetType: {}}, ch)
		close(ch)
	}()

	// Read one item then cancel.
	<-ch
	cancel()

	err := <-done
	assert.NoError(t, err)
}

func TestHandleErr(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		err       error
		expectNil bool
		expectIs  error
	}{
		"nil error": {
			err:       nil,
			expectNil: true,
		},
		"context canceled": {
			err:       context.Canceled,
			expectNil: true,
		},
		"regular error": {
			err:      errors.New("something failed"),
			expectIs: ErrNexusSource,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			result := handleErr(tc.err)
			if tc.expectNil {
				assert.NoError(t, result)
				return
			}
			require.Error(t, result)
			assert.ErrorIs(t, result, tc.expectIs)
		})
	}
}

func TestFlattenComponentAsset(t *testing.T) {
	t.Parallel()

	component := map[string]any{
		"id":         "comp-id",
		"repository": "maven-central",
		"format":     "maven2",
		"group":      "org.example",
		"name":       "mylib",
		"version":    "2.0.0",
		"tags":       []any{"stable"},
		"assets":     []any{map[string]any{"id": "a1"}, map[string]any{"id": "a2"}},
	}

	asset := map[string]any{
		"id":          "a1",
		"path":        "org/example/mylib/2.0.0/mylib-2.0.0.jar",
		"downloadUrl": "https://nexus.example.com/repo/mylib-2.0.0.jar",
		"checksum":    map[string]any{"sha256": "abc"},
	}

	result := flattenComponentAsset(component, asset, "nexus.example.com")

	assert.Equal(t, "nexus.example.com", result["host"])
	assert.Equal(t, "comp-id", result["id"])
	assert.Equal(t, "maven-central", result["repository"])
	assert.Equal(t, "maven2", result["format"])
	assert.Equal(t, "org.example", result["group"])
	assert.Equal(t, "mylib", result["name"])
	assert.Equal(t, "2.0.0", result["version"])
	assert.Equal(t, []any{"stable"}, result["tags"])
	assert.Equal(t, asset, result["asset"])
	assert.Nil(t, result["assets"], "original assets array must not be in the flattened map")
}
