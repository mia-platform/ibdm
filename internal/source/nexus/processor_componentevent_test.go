// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package nexus

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

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
			"repositoryName":"npm-proxy",
			"action":"CREATED",
			"component":{
				"id":"08909bf0",
				"componentId":"bnBtLXByb3h5",
				"format":"npm",
				"name":"angular2",
				"group":"types",
				"version":"0.0.2"
			}
		}`)

		payload, err := parseComponentEvent(body)
		require.NoError(t, err)
		assert.Equal(t, "npm-proxy", payload.RepositoryName)
		assert.Equal(t, "CREATED", payload.Action)
		assert.Equal(t, "08909bf0", payload.Component.ID)
		assert.Equal(t, "bnBtLXByb3h5", payload.Component.ComponentID)
		assert.Equal(t, "npm", payload.Component.Format)
		assert.Equal(t, "angular2", payload.Component.Name)
		assert.Equal(t, "types", payload.Component.Group)
		assert.Equal(t, "0.0.2", payload.Component.Version)
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
	body := []byte(`{"action":"CREATED","repositoryName":"repo","component":{"componentId":"id1","format":"npm","name":"pkg","version":"1.0.0"}}`)

	typesToStream := map[string]source.Extra{}

	data, err := p.process(t.Context(), nil, "nexus.example.com", typesToStream, body)
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

func TestComponentEventProcessorUnknownAction(t *testing.T) {
	t.Parallel()

	p := &componentEventProcessor{}
	body := []byte(`{"action":"UPDATED","repositoryName":"repo","component":{"componentId":"id1","format":"npm","name":"pkg","version":"1.0.0"}}`)
	typesToStream := map[string]source.Extra{componentAssetType: {}}

	data, err := p.process(t.Context(), nil, "nexus.example.com", typesToStream, body)
	require.NoError(t, err)
	assert.Nil(t, data)
}

func TestComponentEventProcessorCreatedWithAPIEnrichment(t *testing.T) {
	timeSource = func() time.Time { return testTime }

	componentID := "bnBtLXByb3h5OjA4OTA5YmYwYzg2Y2Y2Yzk2MDBhYWRlODllMWM1ZTI1"
	apiComponent := map[string]any{
		"id":         componentID,
		"repository": "npm-proxy",
		"format":     "npm",
		"group":      "types",
		"name":       "angular2",
		"version":    "0.0.2",
		"assets": []any{
			map[string]any{
				"id":          "asset1",
				"downloadUrl": "http://nexus/angular2/0.0.2/angular2-0.0.2.tgz",
				"path":        "angular2/0.0.2/angular2-0.0.2.tgz",
				"repository":  "npm-proxy",
				"format":      "npm",
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, url.PathEscape(componentID))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(apiComponent)
	}))
	t.Cleanup(server.Close)

	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(apiComponent)
	}))

	p := &componentEventProcessor{}
	body := []byte(`{
		"action":"CREATED",
		"repositoryName":"npm-proxy",
		"component":{
			"id":"08909bf0",
			"componentId":"` + componentID + `",
			"format":"npm",
			"name":"angular2",
			"group":"types",
			"version":"0.0.2"
		}
	}`)
	typesToStream := map[string]source.Extra{componentAssetType: {}}

	data, err := p.process(t.Context(), c, "nexus.example.com", typesToStream, body)
	require.NoError(t, err)
	require.Len(t, data, 1)
	assert.Equal(t, componentAssetType, data[0].Type)
	assert.Equal(t, source.DataOperationUpsert, data[0].Operation)
	assert.Equal(t, testTime, data[0].Time)
	assert.Equal(t, "nexus.example.com", data[0].Values["host"])
	assert.Equal(t, componentID, data[0].Values["id"])
	assert.Equal(t, "npm-proxy", data[0].Values["repository"])
	assert.NotNil(t, data[0].Values["asset"])
}

func TestComponentEventProcessorCreatedAPIFallback(t *testing.T) {
	timeSource = func() time.Time { return testTime }

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
	body := []byte(`{
		"action":"CREATED",
		"repositoryName":"npm-proxy",
		"component":{
			"id":"08909bf0",
			"componentId":"` + componentID + `",
			"format":"npm",
			"name":"angular2",
			"group":"types",
			"version":"0.0.2"
		}
	}`)
	typesToStream := map[string]source.Extra{componentAssetType: {}}

	data, err := p.process(t.Context(), c, "nexus.example.com", typesToStream, body)
	require.NoError(t, err)
	require.Len(t, data, 1)
	assert.Equal(t, componentAssetType, data[0].Type)
	assert.Equal(t, source.DataOperationUpsert, data[0].Operation)
	assert.Equal(t, componentID, data[0].Values["id"])
	assert.Equal(t, "npm-proxy", data[0].Values["repository"])
}

func TestComponentEventProcessorDeleted(t *testing.T) {
	timeSource = func() time.Time { return testTime }

	p := &componentEventProcessor{}
	componentID := "bnBtLXByb3h5OjA4OTA5YmYwYzg2Y2Y2Yzk2MDBhYWRlODllMWM1ZTI1"
	body := []byte(`{
		"action":"DELETED",
		"repositoryName":"npm-proxy",
		"component":{
			"id":"08909bf0",
			"componentId":"` + componentID + `",
			"format":"npm",
			"name":"angular2",
			"group":"types",
			"version":"0.0.2"
		}
	}`)
	typesToStream := map[string]source.Extra{componentAssetType: {}}

	data, err := p.process(t.Context(), nil, "nexus.example.com", typesToStream, body)
	require.NoError(t, err)
	require.Len(t, data, 1)
	assert.Equal(t, componentAssetType, data[0].Type)
	assert.Equal(t, source.DataOperationDelete, data[0].Operation)
	assert.Equal(t, testTime, data[0].Time)
	assert.Equal(t, "nexus.example.com", data[0].Values["host"])
	assert.Equal(t, componentID, data[0].Values["id"])
	assert.Equal(t, "npm-proxy", data[0].Values["repository"])
	assert.Equal(t, "npm", data[0].Values["format"])
	assert.Equal(t, "types", data[0].Values["group"])
	assert.Equal(t, "angular2", data[0].Values["name"])
	assert.Equal(t, "0.0.2", data[0].Values["version"])
}
