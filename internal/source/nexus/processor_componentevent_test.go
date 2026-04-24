// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package nexus

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/source"
)

func TestParseComponentEvent(t *testing.T) {
	t.Parallel()

	t.Run("valid payload", func(t *testing.T) {
		t.Parallel()

		body := []byte(`{
			"timestamp":"2016-11-14T19:32:13.515+0000",
			"nodeId":"7FFA7361",
			"initiator":"anonymous/127.0.0.1",
			"repositoryName":"docker-hosted",
			"action":"CREATED",
			"component":{
				"id":"08909bf0",
				"componentId":"docker-hosted:component-001",
				"format":"docker",
				"name":"my-image",
				"group":"mygroup",
				"version":"1.0.0"
			}
		}`)

		payload, err := parseComponentEvent(body)
		require.NoError(t, err)
		assert.Equal(t, "docker-hosted", payload.RepositoryName)
		assert.Equal(t, "CREATED", payload.Action)
		assert.Equal(t, "08909bf0", payload.Component.ID)
		assert.Equal(t, "docker-hosted:component-001", payload.Component.ComponentID)
		assert.Equal(t, "docker", payload.Component.Format)
		assert.Equal(t, "my-image", payload.Component.Name)
		assert.Equal(t, "mygroup", payload.Component.Group)
		assert.Equal(t, "1.0.0", payload.Component.Version)
	})

	t.Run("invalid JSON", func(t *testing.T) {
		t.Parallel()

		_, err := parseComponentEvent([]byte(`not json`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse webhook body")
	})
}

func TestComponentEventProcessorTypeNotRequested(t *testing.T) {
	t.Parallel()

	p := &componentEventProcessor{}
	body := []byte(`{"action":"CREATED","repositoryName":"docker-hosted","component":{"componentId":"id1","format":"docker","name":"img","version":"1.0.0"}}`)

	data, err := p.process(t.Context(), nil, "nexus.example.com", map[string]source.Extra{}, body)
	require.NoError(t, err)
	assert.Nil(t, data)
}

func TestComponentEventProcessorMalformedBody(t *testing.T) {
	t.Parallel()

	p := &componentEventProcessor{}
	typesToStream := map[string]source.Extra{componentAssetType: {}}

	data, err := p.process(t.Context(), nil, "nexus.example.com", typesToStream, []byte(`not json`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse webhook body")
	assert.Nil(t, data)
}

func TestComponentEventProcessorNonDockerFormatSkipped(t *testing.T) {
	t.Parallel()

	p := &componentEventProcessor{}
	// npm format — must be skipped regardless of action or requested types.
	body := []byte(`{"action":"CREATED","repositoryName":"npm-proxy","component":{"componentId":"id1","format":"npm","name":"angular2","version":"0.0.2"}}`)
	typesToStream := map[string]source.Extra{componentAssetType: {}, dockerImageType: {}}

	data, err := p.process(t.Context(), nil, "nexus.example.com", typesToStream, body)
	require.NoError(t, err)
	assert.Nil(t, data)
}

func TestComponentEventProcessorUnknownAction(t *testing.T) {
	t.Parallel()

	p := &componentEventProcessor{}
	body := []byte(`{"timestamp":"2025-03-01T12:00:00Z","action":"UPDATED","repositoryName":"docker-hosted","component":{"componentId":"id1","format":"docker","name":"img","version":"1.0.0"}}`)
	typesToStream := map[string]source.Extra{componentAssetType: {}}

	data, err := p.process(t.Context(), nil, "nexus.example.com", typesToStream, body)
	require.NoError(t, err)
	assert.Nil(t, data)
}

func TestComponentEventProcessorCreatedOnlyComponentAsset(t *testing.T) {
	componentID := "docker-hosted:component-id-001"
	apiComponent := map[string]any{
		"id":         componentID,
		"repository": "docker-hosted",
		"format":     "docker",
		"group":      "",
		"name":       "my-image",
		"version":    "1.0.0",
		"assets": []any{
			map[string]any{
				"id":         "asset1",
				"path":       "v2/my-image/manifests/1.0.0",
				"repository": "docker-hosted",
				"format":     "docker",
			},
		},
	}

	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(apiComponent)
	}))

	p := &componentEventProcessor{}
	body := []byte(`{"timestamp":"2025-03-01T12:00:00Z","action":"CREATED","repositoryName":"docker-hosted","component":{"id":"raw-id","componentId":"` + componentID + `","format":"docker","name":"my-image","group":"","version":"1.0.0"}}`)
	typesToStream := map[string]source.Extra{componentAssetType: {}}

	data, err := p.process(t.Context(), c, "nexus.example.com", typesToStream, body)
	require.NoError(t, err)
	require.Len(t, data, 1)
	assert.Equal(t, componentAssetType, data[0].Type)
	assert.Equal(t, source.DataOperationUpsert, data[0].Operation)
	assert.Equal(t, testTime, data[0].Time)
	assert.Equal(t, "nexus.example.com", data[0].Values["host"])
	assert.Equal(t, componentID, data[0].Values["id"])
	assert.Equal(t, "docker-hosted", data[0].Values["repository"])
	assert.NotNil(t, data[0].Values["asset"])
}

func TestComponentEventProcessorCreatedOnlyDockerImage(t *testing.T) {
	componentID := "docker-hosted:component-id-002"
	apiComponent := map[string]any{
		"id":         componentID,
		"repository": "docker-hosted",
		"format":     "docker",
		"name":       "my-image",
		"version":    "2.0.0",
		"assets":     []any{},
	}

	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(apiComponent)
	}))

	p := &componentEventProcessor{}
	body := []byte(`{"timestamp":"2025-03-01T12:00:00Z","action":"CREATED","repositoryName":"docker-hosted","component":{"id":"raw-id","componentId":"` + componentID + `","format":"docker","name":"my-image","group":"","version":"2.0.0"}}`)
	typesToStream := map[string]source.Extra{dockerImageType: {}}

	data, err := p.process(t.Context(), c, "nexus.example.com", typesToStream, body)
	require.NoError(t, err)
	require.Len(t, data, 1)
	assert.Equal(t, dockerImageType, data[0].Type)
	assert.Equal(t, source.DataOperationUpsert, data[0].Operation)
	assert.Equal(t, testTime, data[0].Time)
	assert.Equal(t, "nexus.example.com", data[0].Values["host"])
	assert.Equal(t, "my-image", data[0].Values["name"])
	assert.Equal(t, "2.0.0", data[0].Values["version"])
}

func TestComponentEventProcessorCreatedBothTypes(t *testing.T) {
	componentID := "docker-hosted:component-id-003"
	apiComponent := map[string]any{
		"id":         componentID,
		"repository": "docker-hosted",
		"format":     "docker",
		"name":       "my-image",
		"version":    "3.0.0",
		"assets": []any{
			map[string]any{"id": "asset1", "path": "v2/my-image/manifests/3.0.0"},
			map[string]any{"id": "asset2", "path": "v2/my-image/blobs/sha256:abc"},
		},
	}

	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(apiComponent)
	}))

	p := &componentEventProcessor{}
	body := []byte(`{"timestamp":"2025-03-01T12:00:00Z","action":"CREATED","repositoryName":"docker-hosted","component":{"id":"raw-id","componentId":"` + componentID + `","format":"docker","name":"my-image","group":"","version":"3.0.0"}}`)
	typesToStream := map[string]source.Extra{dockerImageType: {}, componentAssetType: {}}

	data, err := p.process(t.Context(), c, "nexus.example.com", typesToStream, body)
	require.NoError(t, err)
	// 1 dockerimage + 2 componentasset (one per asset)
	require.Len(t, data, 3)

	dockerImages := filterByType(data, dockerImageType)
	componentAssets := filterByType(data, componentAssetType)
	require.Len(t, dockerImages, 1)
	require.Len(t, componentAssets, 2)

	assert.Equal(t, source.DataOperationUpsert, dockerImages[0].Operation)
	assert.Equal(t, "my-image", dockerImages[0].Values["name"])
	assert.Equal(t, "3.0.0", dockerImages[0].Values["version"])

	for _, ca := range componentAssets {
		assert.Equal(t, source.DataOperationUpsert, ca.Operation)
		assert.NotNil(t, ca.Values["asset"])
	}
}

func TestComponentEventProcessorCreatedAPIFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	u, err := url.Parse(server.URL)
	require.NoError(t, err)

	c := &client{
		baseURL:       u,
		tokenName:     "tok",
		tokenPasscode: "pass",
		httpClient:    server.Client(),
	}

	p := &componentEventProcessor{}
	componentID := "fallback-component-id"
	body := []byte(`{"timestamp":"2025-03-01T12:00:00.073+00:00","action":"CREATED","repositoryName":"docker-hosted","component":{"id":"raw-id","componentId":"` + componentID + `","format":"docker","name":"my-image","group":"","version":"1.0.0"}}`)
	typesToStream := map[string]source.Extra{componentAssetType: {}}

	data, err := p.process(t.Context(), c, "nexus.example.com", typesToStream, body)
	require.NoError(t, err)
	require.Len(t, data, 1)
	assert.Equal(t, componentAssetType, data[0].Type)
	assert.Equal(t, source.DataOperationUpsert, data[0].Operation)
	assert.Equal(t, componentID, data[0].Values["id"])
	assert.Equal(t, "docker-hosted", data[0].Values["repository"])
}

func TestComponentEventProcessorDeletedOnlyComponentAsset(t *testing.T) {
	p := &componentEventProcessor{}
	componentID := "docker-hosted:component-del-id"
	body := []byte(`{"timestamp":"2025-03-01T12:00:00Z","action":"DELETED","repositoryName":"docker-hosted","component":{"id":"raw-id","componentId":"` + componentID + `","format":"docker","name":"my-image","group":"","version":"1.0.0"}}`)
	typesToStream := map[string]source.Extra{componentAssetType: {}}

	data, err := p.process(t.Context(), nil, "nexus.example.com", typesToStream, body)
	require.NoError(t, err)
	require.Len(t, data, 1)
	assert.Equal(t, componentAssetType, data[0].Type)
	assert.Equal(t, source.DataOperationDelete, data[0].Operation)
	assert.Equal(t, testTime, data[0].Time)
	assert.Equal(t, "nexus.example.com", data[0].Values["host"])
	assert.Equal(t, componentID, data[0].Values["id"])
	assert.Equal(t, "docker-hosted", data[0].Values["repository"])
	assert.Equal(t, "docker", data[0].Values["format"])
	assert.Equal(t, "my-image", data[0].Values["name"])
	assert.Equal(t, "1.0.0", data[0].Values["version"])
}

func TestComponentEventProcessorDeletedOnlyDockerImage(t *testing.T) {
	p := &componentEventProcessor{}
	componentID := "docker-hosted:component-del-id-2"
	body := []byte(`{"timestamp":"2025-03-01T12:00:00Z","action":"DELETED","repositoryName":"docker-hosted","component":{"id":"raw-id","componentId":"` + componentID + `","format":"docker","name":"my-image","group":"","version":"2.0.0"}}`)
	typesToStream := map[string]source.Extra{dockerImageType: {}}

	data, err := p.process(t.Context(), nil, "nexus.example.com", typesToStream, body)
	require.NoError(t, err)
	require.Len(t, data, 1)
	assert.Equal(t, dockerImageType, data[0].Type)
	assert.Equal(t, source.DataOperationDelete, data[0].Operation)
	assert.Equal(t, "nexus.example.com", data[0].Values["host"])
	assert.Equal(t, "my-image", data[0].Values["name"])
	assert.Equal(t, "2.0.0", data[0].Values["version"])
}

func TestComponentEventProcessorDeletedBothTypes(t *testing.T) {
	p := &componentEventProcessor{}
	componentID := "docker-hosted:component-del-id-3"
	body := []byte(`{"timestamp":"2025-03-01T12:00:00Z","action":"DELETED","repositoryName":"docker-hosted","component":{"id":"raw-id","componentId":"` + componentID + `","format":"docker","name":"my-image","group":"mygroup","version":"3.0.0"}}`)
	typesToStream := map[string]source.Extra{dockerImageType: {}, componentAssetType: {}}

	data, err := p.process(t.Context(), nil, "nexus.example.com", typesToStream, body)
	require.NoError(t, err)
	require.Len(t, data, 2)

	dockerImages := filterByType(data, dockerImageType)
	componentAssets := filterByType(data, componentAssetType)
	require.Len(t, dockerImages, 1)
	require.Len(t, componentAssets, 1)

	assert.Equal(t, source.DataOperationDelete, dockerImages[0].Operation)
	assert.Equal(t, "my-image", dockerImages[0].Values["name"])

	assert.Equal(t, source.DataOperationDelete, componentAssets[0].Operation)
	assert.Equal(t, componentID, componentAssets[0].Values["id"])
	assert.Equal(t, "mygroup", componentAssets[0].Values["group"])
}

func TestComponentEventProcessorMissingTimestamp(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		body    []byte
		errText string
	}{
		"missing timestamp field": {
			body:    []byte(`{"action":"CREATED","repositoryName":"docker-hosted","component":{"componentId":"id1","format":"docker","name":"img","version":"1.0.0"}}`),
			errText: "timestamp is missing",
		},
		"invalid timestamp format": {
			body:    []byte(`{"timestamp":"not-a-date","action":"CREATED","repositoryName":"docker-hosted","component":{"componentId":"id1","format":"docker","name":"img","version":"1.0.0"}}`),
			errText: "invalid",
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p := &componentEventProcessor{}
			typesToStream := map[string]source.Extra{componentAssetType: {}}
			data, err := p.process(t.Context(), nil, "nexus.example.com", typesToStream, tc.body)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.errText)
			assert.Nil(t, data)
		})
	}
}

// filterByType returns items from data whose Type matches the given type.
func filterByType(data []source.Data, typ string) []source.Data {
	var result []source.Data
	for _, d := range data {
		if d.Type == typ {
			result = append(result, d)
		}
	}
	return result
}
