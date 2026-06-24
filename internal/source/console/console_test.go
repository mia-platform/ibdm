// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package console

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/source"
	"github.com/mia-platform/ibdm/internal/source/console/service"
)

var (
	testTime = time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC)
)

func init() {
	timeSource = func() time.Time {
		return testTime
	}
}

func signedHeaders(body []byte, secret, sigPrefix string) http.Header {
	hasher := sha256.New()
	hasher.Write(body)
	hasher.Write([]byte(secret))
	sig := sigPrefix + hex.EncodeToString(hasher.Sum(nil))
	h := http.Header{}
	h.Add(authHeaderName, sig)
	return h
}

func TestSource_NewSource(t *testing.T) {
	t.Run("fails when CONSOLE_WEBHOOK_PATH is missing", func(t *testing.T) {
		s, err := NewSource()
		require.Error(t, err)
		require.Nil(t, s)
	})

	t.Run("succeeds with valid config", func(t *testing.T) {
		t.Setenv("CONSOLE_WEBHOOK_PATH", "/webhook")
		t.Setenv("CONSOLE_WEBHOOK_SECRET", "secret")
		t.Setenv("CONSOLE_ENDPOINT", "http://example.com")
		s, err := NewSource()
		require.NoError(t, err)
		require.NotNil(t, s)
		require.Equal(t, "/webhook", s.c.config.WebhookPath)
	})
}

func TestSource_GetWebhook(t *testing.T) {
	t.Parallel()
	t.Run("fails when CONSOLE_WEBHOOK_SECRET is missing", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()

		s := Source{
			c: &webhookClient{
				config: webhookConfig{
					WebhookPath: "/webhook",
				},
			},
		}

		results := make(chan source.Data, 1)
		typesToStream := map[string]source.Extra{"nothing": {}}

		webhook, err := s.GetWebhook(ctx, typesToStream, results)
		require.ErrorIs(t, err, ErrWebhookSecretMissing)
		require.Equal(t, source.Webhook{}, webhook)
	})

	handlerTests := map[string]struct {
		typesToStream    map[string]source.Extra
		rawBody          []byte
		eventPayload     map[string]any
		signaturePrefix  string
		expectHandlerErr bool
		expectedData     *source.Data
	}{
		"successfully creates webhook and processes events": {
			typesToStream: map[string]source.Extra{"project": {}},
			eventPayload: map[string]any{
				"eventName": "project_created",
				"payload": map[string]any{
					"name": "test-project",
					"key":  "value",
				},
			},
			expectedData: &source.Data{
				Type:      "project",
				Operation: source.DataOperationUpsert,
				Values: map[string]any{
					"name": "test-project",
					"key":  "value",
				},
			},
		},
		"ignores events not in typesToStream": {
			typesToStream: map[string]source.Extra{"project": {}},
			eventPayload: map[string]any{
				"eventName": "order_created",
				"payload": map[string]any{
					"name": "test-order",
					"key":  "value",
				},
			},
			signaturePrefix: "sha256=",
		},
		"returns error on invalid json": {
			typesToStream:    map[string]source.Extra{"user": {}},
			rawBody:          []byte(`{invalid-json`),
			signaturePrefix:  "sha256=",
			expectHandlerErr: true,
		},
	}

	for name, tc := range handlerTests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()

			s := Source{
				c: &webhookClient{
					config: webhookConfig{
						WebhookPath:   "/webhook",
						WebhookSecret: "secret",
					},
				},
			}

			results := make(chan source.Data, 1)

			webhook, err := s.GetWebhook(ctx, tc.typesToStream, results)
			require.NoError(t, err)
			require.Equal(t, "/webhook", webhook.Path)
			require.Equal(t, http.MethodPost, webhook.Method)
			require.NotNil(t, webhook.Handler)

			body := tc.rawBody
			if body == nil {
				body, err = json.Marshal(tc.eventPayload)
				require.NoError(t, err)
			}

			headers := signedHeaders(body, "secret", tc.signaturePrefix)
			err = webhook.Handler(ctx, headers, body)

			if tc.expectHandlerErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			if tc.expectedData != nil {
				ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
				defer cancel()

				select {
				case data := <-results:
					require.Equal(t, tc.expectedData.Type, data.Type)
					require.Equal(t, tc.expectedData.Operation, data.Operation)
					require.Equal(t, tc.expectedData.Values, data.Values)
				case <-ctx.Done():
					t.Fatal("Timeout waiting for message processing: expected data in channel")
				}
			} else {
				select {
				case <-results:
					t.Fatal("did not expect data in channel")
				default:
				}
			}
		})
	}
}

func Test_DoChain(t *testing.T) {
	tests := map[string]struct {
		event         event
		handler       http.HandlerFunc
		expectedError error
		expectedData  []source.Data
	}{
		"configuration event": {
			event: event{
				EventName:      "configuration_created",
				EventTimestamp: 1672531200000, // 2023-01-01 00:00:00 UTC
				Payload: map[string]any{
					"projectId":    "p1",
					"revisionName": "r1",
				},
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/backend/projects/p1":
					json.NewEncoder(w).Encode(map[string]any{
						"_id":           "p1",
						"projectId":     "projectId",
						"name":          "name",
						"defaultBranch": "r1",
						"tenantId":      "",
						"info": map[string]any{
							"teamContact": "contact",
						},
					})
				case "/backend/projects/p1/revisions/r1/configuration":
					json.NewEncoder(w).Encode(map[string]any{
						"key": "value",
						"services": map[string]any{
							"service-1": map[string]any{
								"name":     "service-1",
								"type":     "custom",
								"advanced": false,
							},
						},
					})
				default:
					w.WriteHeader(http.StatusNotFound)
					return
				}
			},
			expectedData: []source.Data{
				{
					Type:      projectResource,
					Operation: source.DataOperationUpsert,
					Time:      time.Unix(1672531200, 0),
					Values: map[string]any{
						"project": map[string]any{
							"_id":           "p1",
							"projectId":     "projectId",
							"name":          "name",
							"defaultBranch": "r1",
							"tenantId":      "",
							"info": map[string]any{
								"teamContact": "contact",
							},
						},
					},
				},
				{
					Type:      revisionResource,
					Operation: source.DataOperationUpsert,
					Time:      time.Unix(1672531200, 0),
					Values: map[string]any{
						"project": map[string]any{
							"_id":       "p1",
							"projectId": "projectId",
							"name":      "name",
							"tenantId":  "",
							"info": map[string]any{
								"teamContact": "contact",
							},
						},
						"revision": map[string]any{
							"name": "r1",
						},
					},
				},
				{
					Type:      serviceResource,
					Operation: source.DataOperationUpsert,
					Time:      time.Unix(1672531200, 0),
					Values: map[string]any{
						"project": map[string]any{
							"_id":       "p1",
							"projectId": "projectId",
							"name":      "name",
							"tenantId":  "",
							"info": map[string]any{
								"teamContact": "contact",
							},
						},
						"revision": map[string]any{
							"name": "r1",
						},
						"service": map[string]any{
							"name":     "service-1",
							"type":     "custom",
							"advanced": false,
						},
					},
				},
			},
		},
		"configuration event no project info": {
			event: event{
				EventName:      "configuration_created",
				EventTimestamp: 1672531200000, // 2023-01-01 00:00:00 UTC
				Payload: map[string]any{
					"projectId":    "p1",
					"revisionName": "r1",
				},
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/backend/projects/p1":
					json.NewEncoder(w).Encode(map[string]any{
						"_id":           "p1",
						"projectId":     "projectId",
						"name":          "name",
						"defaultBranch": "r1",
						"tenantId":      "",
						"info":          nil,
					})
				case "/backend/projects/p1/revisions/r1/configuration":
					json.NewEncoder(w).Encode(map[string]any{
						"key": "value",
						"services": map[string]any{
							"service-1": map[string]any{
								"name":     "service-1",
								"type":     "custom",
								"advanced": false,
							},
						},
					})
				default:
					w.WriteHeader(http.StatusNotFound)
					return
				}
			},
			expectedData: []source.Data{
				{
					Type:      projectResource,
					Operation: source.DataOperationUpsert,
					Time:      time.Unix(1672531200, 0),
					Values: map[string]any{
						"project": map[string]any{
							"_id":           "p1",
							"projectId":     "projectId",
							"name":          "name",
							"defaultBranch": "r1",
							"tenantId":      "",
							"info":          nil,
						},
					},
				},
				{
					Type:      revisionResource,
					Operation: source.DataOperationUpsert,
					Time:      time.Unix(1672531200, 0),
					Values: map[string]any{
						"project": map[string]any{
							"_id":       "p1",
							"projectId": "projectId",
							"name":      "name",
							"tenantId":  "",
							"info":      nil,
						},
						"revision": map[string]any{
							"name": "r1",
						},
					},
				},
				{
					Type:      serviceResource,
					Operation: source.DataOperationUpsert,
					Time:      time.Unix(1672531200, 0),
					Values: map[string]any{
						"project": map[string]any{
							"_id":       "p1",
							"projectId": "projectId",
							"name":      "name",
							"tenantId":  "",
							"info":      nil,
						},
						"revision": map[string]any{
							"name": "r1",
						},
						"service": map[string]any{
							"name":     "service-1",
							"type":     "custom",
							"advanced": false,
						},
					},
				},
			},
		},
		"project event: delete": {
			event: event{
				EventName:      "project_deleted",
				EventTimestamp: 1672531200000,
				Payload: map[string]any{
					"id": "123",
				},
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotImplemented)
			},
			expectedData: []source.Data{
				{
					Type:      "project",
					Operation: source.DataOperationDelete,
					Values: map[string]any{
						"id": "123",
					},
					Time: time.Unix(1672531200, 0),
				},
			},
		},
		"other event": {
			event: event{
				EventName:      "other_resource_updated",
				EventTimestamp: 1672531200000,
				Payload: map[string]any{
					"foo": "bar",
				},
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotImplemented)
			},
			expectedData: []source.Data{
				{
					Type:      "other_resource",
					Operation: source.DataOperationUpsert,
					Values: map[string]any{
						"foo": "bar",
					},
					Time: time.Unix(1672531200, 0),
				},
			},
		},
	}

	for testName, test := range tests {
		t.Run(testName, func(t *testing.T) {
			ctx := t.Context()

			server := httptest.NewServer(test.handler)
			defer server.Close()
			t.Setenv("CONSOLE_ENDPOINT", server.URL)

			cs, err := service.NewConsoleService()
			require.NoError(t, err)
			src := Source{cs: cs}

			ch := make(chan source.Data, len(test.expectedData)+1)

			typesToStream := map[string]source.Extra{
				serviceResource:  {},
				revisionResource: {},
				projectResource:  {},
				"other_resource": {},
			}

			err = src.handleEvent(ctx, test.event, typesToStream, ch)
			if test.expectedError != nil {
				require.ErrorIs(t, err, test.expectedError)
				return
			}
			require.NoError(t, err)
			close(ch)

			var data []source.Data
			for d := range ch {
				if d.Type == "configuration" {
					d.Time = time.Time{}
				}
				data = append(data, d)
			}

			expected := make([]source.Data, len(test.expectedData))
			copy(expected, test.expectedData)
			for i := range expected {
				if expected[i].Type == "configuration" {
					expected[i].Time = time.Time{}
				}
			}

			require.ElementsMatch(t, expected, data)
		})
	}
}

func TestSource_listAssets(t *testing.T) {
	t.Run("successfully lists projects and configurations", func(t *testing.T) {
		ctx := t.Context()

		project1 := map[string]any{
			"_id":       "p1",
			"projectId": "project-1",
			"name":      "name",
			"tenantId":  "tenant-1",
			"info": map[string]any{
				"teamContact": "contact",
			},
		}

		revision1 := map[string]any{
			"name": "r1",
		}
		service1 := map[string]any{
			"name":     "service-1",
			"type":     "custom",
			"advanced": false,
		}

		service2 := map[string]any{
			"name":     "service-2",
			"advanced": true,
		}

		expectedData := []source.Data{
			{
				Type:      projectResource,
				Operation: source.DataOperationUpsert,
				Time:      testTime,
				Values: map[string]any{
					"project": map[string]any{
						"_id":           "p1",
						"projectId":     "project-1",
						"name":          "name",
						"tenantId":      "tenant-1",
						"defaultBranch": "r1",
						"info": map[string]any{
							"teamContact": "contact",
						},
					},
				},
			},
			{
				Type:      revisionResource,
				Operation: source.DataOperationUpsert,
				Time:      testTime,
				Values: map[string]any{
					"project":  project1,
					"revision": revision1,
				},
			},
			{
				Type:      serviceResource,
				Operation: source.DataOperationUpsert,
				Time:      testTime,
				Values: map[string]any{
					"project":  project1,
					"revision": revision1,
					"service":  service1,
				},
			},
		}

		handler := func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/backend/projects/":
				projectResponse := maps.Clone(project1)
				projectResponse["defaultBranch"] = "r1"
				json.NewEncoder(w).Encode([]map[string]any{projectResponse})
			case "/backend/projects/p1/revisions":
				json.NewEncoder(w).Encode([]map[string]any{revision1})
			case "/backend/projects/p1/revisions/r1/configuration":
				json.NewEncoder(w).Encode(map[string]any{
					"key": "value",
					"fastDataConfig": map[string]any{
						"castFunctions": "some-code",
					},
					"services": map[string]any{
						"service-1": service1,
						"service-2": service2,
					},
				})
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}

		server := httptest.NewServer(http.HandlerFunc(handler))
		defer server.Close()
		t.Setenv("CONSOLE_ENDPOINT", server.URL)
		t.Setenv("CONSOLE_WEBHOOK_PATH", "/webhook")

		s, err := NewSource()
		require.NoError(t, err)

		typesToSync := map[string]source.Extra{
			projectResource:  {},
			revisionResource: {},
			serviceResource:  {},
		}

		data, err := s.listAssets(ctx, typesToSync)
		require.NoError(t, err)
		assert.Equal(t, expectedData, data)
	})

	errorTests := map[string]struct {
		handler     http.HandlerFunc
		typesToSync map[string]source.Extra
	}{
		"returns error when GetProjects fails": {
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			typesToSync: map[string]source.Extra{projectResource: {}},
		},
		"returns error when GetRevisions fails during configuration sync": {
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/backend/projects/" {
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode([]map[string]any{{"_id": "p1"}})
					return
				}
				w.WriteHeader(http.StatusInternalServerError)
			},
			typesToSync: map[string]source.Extra{revisionResource: {}},
		},
		"returns error when GetConfiguration fails during configuration sync": {
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/backend/projects/":
					json.NewEncoder(w).Encode([]map[string]any{{"_id": "p1"}})
				case "/backend/projects/p1/revisions":
					json.NewEncoder(w).Encode([]map[string]any{{"name": "r1"}})
				default:
					w.WriteHeader(http.StatusInternalServerError)
				}
			},
			typesToSync: map[string]source.Extra{serviceResource: {}},
		},
	}

	for name, tc := range errorTests {
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()

			server := httptest.NewServer(tc.handler)
			defer server.Close()
			t.Setenv("CONSOLE_ENDPOINT", server.URL)
			t.Setenv("CONSOLE_WEBHOOK_PATH", "/webhook")

			s, err := NewSource()
			require.NoError(t, err)

			_, err = s.listAssets(ctx, tc.typesToSync)
			require.ErrorIs(t, err, ErrRetrievingAssets)
		})
	}
}

func TestSource_listClusters(t *testing.T) {
	tenant1 := map[string]any{"companyId": "t1", "name": "Tenant One"}
	cluster1 := map[string]any{
		"_id":       "c1",
		"clusterId": "demo-azure",
		"connection": map[string]any{
			"url": "https://paas-demo.hcp.northeurope.azmk8s.io",
		},
		"distribution": "AKS",
		"runtimeInfo": map[string]any{
			"cpuCores":   float64(4),
			"nodesCount": float64(2),
		},
		"tenantId": "t1",
		"vendor":   "Azure",
		linkedProjectsField: []any{
			map[string]any{"_id": "p1", "name": "Project One", "projectId": "proj1"},
			map[string]any{"_id": "p2", "name": "Project Two", "projectId": "proj2"},
		},
	}
	clusterWithoutLinkedProjects := map[string]any{
		"_id":       "c1",
		"clusterId": "demo-azure",
		"connection": map[string]any{
			"url": "https://paas-demo.hcp.northeurope.azmk8s.io",
		},
		"distribution": "AKS",
		"runtimeInfo": map[string]any{
			"cpuCores":   float64(4),
			"nodesCount": float64(2),
		},
		"tenantId": "t1",
		"vendor":   "Azure",
	}

	makeHandler := func(tenants []map[string]any, clusters map[string][]map[string]any) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/user/companies":
				json.NewEncoder(w).Encode(tenants)
			default:
				for tenantID, cls := range clusters {
					if r.URL.Path == "/tenants/"+tenantID+"/clusters/" {
						json.NewEncoder(w).Encode(cls)
						return
					}
				}
				w.WriteHeader(http.StatusNotFound)
			}
		}
	}

	t.Run("successfully lists clusters and relationships", func(t *testing.T) {
		ctx := t.Context()

		handler := makeHandler(
			[]map[string]any{tenant1},
			map[string][]map[string]any{"t1": {cluster1}},
		)
		server := httptest.NewServer(handler)
		defer server.Close()
		t.Setenv("CONSOLE_ENDPOINT", server.URL)
		t.Setenv("CONSOLE_WEBHOOK_PATH", "/webhook")

		s, err := NewSource()
		require.NoError(t, err)

		typesToSync := map[string]source.Extra{
			clusterResource:                    {},
			clusterProjectRelationshipResource: {},
		}

		data, err := s.listAssets(ctx, typesToSync)
		require.NoError(t, err)
		require.Len(t, data, 3) // 1 cluster + 2 relationships

		clusterItems := []source.Data{}
		relItems := []source.Data{}
		for _, d := range data {
			switch d.Type {
			case clusterResource:
				clusterItems = append(clusterItems, d)
			case clusterProjectRelationshipResource:
				relItems = append(relItems, d)
			}
		}
		require.Len(t, clusterItems, 1)
		require.Len(t, relItems, 2)

		assert.Equal(t, map[string]any{clusterResource: clusterWithoutLinkedProjects}, clusterItems[0].Values)
		assert.Equal(t, clusterProjectRelationshipResource, relItems[0].Type)
		assert.Equal(t, clusterWithoutLinkedProjects, relItems[0].Values[clusterResource])
	})

	t.Run("skips tenant with missing companyId", func(t *testing.T) {
		ctx := t.Context()

		handler := makeHandler(
			[]map[string]any{{"name": "No ID Tenant"}},
			map[string][]map[string]any{},
		)
		server := httptest.NewServer(handler)
		defer server.Close()
		t.Setenv("CONSOLE_ENDPOINT", server.URL)
		t.Setenv("CONSOLE_WEBHOOK_PATH", "/webhook")

		s, err := NewSource()
		require.NoError(t, err)

		data, err := s.listAssets(ctx, map[string]source.Extra{clusterResource: {}})
		require.NoError(t, err)
		assert.Empty(t, data)
	})

	t.Run("only cluster type requested omits relationships", func(t *testing.T) {
		ctx := t.Context()

		handler := makeHandler(
			[]map[string]any{tenant1},
			map[string][]map[string]any{"t1": {cluster1}},
		)
		server := httptest.NewServer(handler)
		defer server.Close()
		t.Setenv("CONSOLE_ENDPOINT", server.URL)
		t.Setenv("CONSOLE_WEBHOOK_PATH", "/webhook")

		s, err := NewSource()
		require.NoError(t, err)

		data, err := s.listAssets(ctx, map[string]source.Extra{clusterResource: {}})
		require.NoError(t, err)
		require.Len(t, data, 1)
		assert.Equal(t, clusterResource, data[0].Type)
	})

	errorTests := map[string]struct {
		handler http.HandlerFunc
	}{
		"returns error when GetTenants fails": {
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
		},
		"returns error when GetClusters fails": {
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/user/companies" {
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode([]map[string]any{tenant1})
					return
				}
				w.WriteHeader(http.StatusInternalServerError)
			},
		},
	}

	for name, tc := range errorTests {
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()

			server := httptest.NewServer(tc.handler)
			defer server.Close()
			t.Setenv("CONSOLE_ENDPOINT", server.URL)
			t.Setenv("CONSOLE_WEBHOOK_PATH", "/webhook")

			s, err := NewSource()
			require.NoError(t, err)

			_, err = s.listAssets(ctx, map[string]source.Extra{clusterResource: {}})
			require.ErrorIs(t, err, ErrRetrievingAssets)
		})
	}
}

func Test_buildClusterData(t *testing.T) {
	t.Parallel()

	input := map[string]any{
		"_id":               "c1",
		"clusterId":         "demo",
		"distribution":      "AKS",
		linkedProjectsField: []any{map[string]any{"_id": "p1"}},
		"tenantId":          "t1",
	}

	result := buildClusterData(input)
	assert.NotContains(t, result, linkedProjectsField)
	assert.Equal(t, "c1", result["_id"])
	assert.Equal(t, "demo", result["clusterId"])
	assert.Equal(t, "AKS", result["distribution"])
	assert.Equal(t, "t1", result["tenantId"])
}

func Test_buildServiceData(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input    map[string]any
		expected map[string]any
	}{
		"no dockerImage field": {
			input:    map[string]any{"name": "svc"},
			expected: map[string]any{"name": "svc"},
		},
		"image without tag gets :latest appended": {
			input:    map[string]any{"dockerImage": "nexus.host/board/app"},
			expected: map[string]any{"dockerImage": "nexus.host/board/app:latest"},
		},
		"image with explicit version is unchanged": {
			input:    map[string]any{"dockerImage": "nexus.host/board/app:8.1.2"},
			expected: map[string]any{"dockerImage": "nexus.host/board/app:8.1.2"},
		},
		"registry with port and no tag gets :latest appended": {
			input:    map[string]any{"dockerImage": "registry:5000/app"},
			expected: map[string]any{"dockerImage": "registry:5000/app:latest"},
		},
		"registry with port and explicit tag is unchanged": {
			input:    map[string]any{"dockerImage": "registry:5000/app:1.0"},
			expected: map[string]any{"dockerImage": "registry:5000/app:1.0"},
		},
		"digest-pinned image is unchanged": {
			input:    map[string]any{"dockerImage": "app@sha256:abc123"},
			expected: map[string]any{"dockerImage": "app@sha256:abc123"},
		},
		"empty dockerImage is unchanged": {
			input:    map[string]any{"dockerImage": ""},
			expected: map[string]any{"dockerImage": ""},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			result := buildServiceData(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func Test_isCustomResourceValid(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input    map[string]any
		expected bool
	}{
		"type custom-resource is valid": {
			input:    map[string]any{"name": "cr-1", "type": "custom-resource"},
			expected: true,
		},
		"type custom is not valid": {
			input:    map[string]any{"name": "svc-1", "type": "custom", "advanced": false},
			expected: false,
		},
		"no type field is not valid": {
			input:    map[string]any{"name": "cr-2"},
			expected: false,
		},
		"type plugin is not valid": {
			input:    map[string]any{"name": "plugin-1", "type": "plugin"},
			expected: false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, isCustomResourceValid(tc.input))
		})
	}
}

func TestSource_listAssets_customResources(t *testing.T) {
	t.Run("lists custom-resources from the default branch", func(t *testing.T) {
		ctx := t.Context()

		project1 := map[string]any{
			"_id":       "p1",
			"projectId": "project-1",
			"name":      "name",
			"tenantId":  "tenant-1",
			"info": map[string]any{
				"teamContact": "contact",
			},
		}

		revision1 := map[string]any{"name": "r1"}

		cr1 := map[string]any{
			"name": "example-custom-resource",
			"type": "custom-resource",
			"meta": map[string]any{
				"kind":       "ExampleCustomKind",
				"apiVersion": "custom-generator.console.mia-platform.eu/v1",
			},
			"spec": map[string]any{
				"example_spec_key": float64(42),
				"parsed":           true,
			},
		}

		// a plain "custom" service that must NOT appear in custom-resource results
		svc1 := map[string]any{
			"name":     "service-1",
			"type":     "custom",
			"advanced": false,
		}

		expectedData := []source.Data{
			{
				Type:      customResourceResource,
				Operation: source.DataOperationUpsert,
				Time:      testTime,
				Values: map[string]any{
					"project":        project1,
					"revision":       revision1,
					"customResource": cr1,
				},
			},
		}

		handler := func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/backend/projects/":
				projectResponse := maps.Clone(project1)
				projectResponse["defaultBranch"] = "r1"
				json.NewEncoder(w).Encode([]map[string]any{projectResponse})
			case "/backend/projects/p1/revisions":
				json.NewEncoder(w).Encode([]map[string]any{revision1})
			case "/backend/projects/p1/revisions/r1/configuration":
				json.NewEncoder(w).Encode(map[string]any{
					"services": map[string]any{
						"example-custom-resource": cr1,
						"service-1":               svc1,
					},
				})
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}

		server := httptest.NewServer(http.HandlerFunc(handler))
		defer server.Close()
		t.Setenv("CONSOLE_ENDPOINT", server.URL)
		t.Setenv("CONSOLE_WEBHOOK_PATH", "/webhook")

		s, err := NewSource()
		require.NoError(t, err)

		typesToSync := map[string]source.Extra{
			customResourceResource: {},
		}

		data, err := s.listAssets(ctx, typesToSync)
		require.NoError(t, err)
		assert.Equal(t, expectedData, data)
	})

	t.Run("skips custom-resources from non-default branches", func(t *testing.T) {
		ctx := t.Context()

		handler := func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/backend/projects/":
				json.NewEncoder(w).Encode([]map[string]any{{
					"_id":           "p1",
					"projectId":     "project-1",
					"name":          "name",
					"tenantId":      "tenant-1",
					"defaultBranch": "main",
				}})
			case "/backend/projects/p1/revisions":
				json.NewEncoder(w).Encode([]map[string]any{{"name": "feature-branch"}})
			case "/backend/projects/p1/revisions/feature-branch/configuration":
				json.NewEncoder(w).Encode(map[string]any{
					"services": map[string]any{
						"cr-1": map[string]any{"name": "cr-1", "type": "custom-resource"},
					},
				})
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}

		server := httptest.NewServer(http.HandlerFunc(handler))
		defer server.Close()
		t.Setenv("CONSOLE_ENDPOINT", server.URL)
		t.Setenv("CONSOLE_WEBHOOK_PATH", "/webhook")

		s, err := NewSource()
		require.NoError(t, err)

		data, err := s.listAssets(ctx, map[string]source.Extra{customResourceResource: {}})
		require.NoError(t, err)
		assert.Empty(t, data)
	})
}

func TestSource_configurationEventChain_customResource(t *testing.T) {
	t.Run("emits custom-resource items on configuration event", func(t *testing.T) {
		ctx := t.Context()

		cr1 := map[string]any{
			"name": "example-custom-resource",
			"type": "custom-resource",
			"meta": map[string]any{
				"kind":       "ExampleCustomKind",
				"apiVersion": "custom-generator.console.mia-platform.eu/v1",
			},
		}

		handler := func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/backend/projects/p1":
				json.NewEncoder(w).Encode(map[string]any{
					"_id":           "p1",
					"projectId":     "projectId",
					"name":          "name",
					"defaultBranch": "r1",
					"tenantId":      "",
					"info":          nil,
				})
			case "/backend/projects/p1/revisions/r1/configuration":
				json.NewEncoder(w).Encode(map[string]any{
					"services": map[string]any{
						"example-custom-resource": cr1,
						"svc-1": map[string]any{
							"name":     "svc-1",
							"type":     "custom",
							"advanced": false,
						},
					},
				})
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}

		server := httptest.NewServer(http.HandlerFunc(handler))
		defer server.Close()
		t.Setenv("CONSOLE_ENDPOINT", server.URL)

		cs, err := service.NewConsoleService()
		require.NoError(t, err)
		src := Source{cs: cs}

		ev := event{
			EventName:      "configuration_created",
			EventTimestamp: 1672531200000,
			Payload: map[string]any{
				"projectId":    "p1",
				"revisionName": "r1",
			},
		}

		ch := make(chan source.Data, 5)
		typesToStream := map[string]source.Extra{
			customResourceResource: {},
		}

		err = src.handleEvent(ctx, ev, typesToStream, ch)
		require.NoError(t, err)
		close(ch)

		var data []source.Data
		for d := range ch {
			data = append(data, d)
		}

		require.Len(t, data, 1)
		assert.Equal(t, customResourceResource, data[0].Type)
		assert.Equal(t, source.DataOperationUpsert, data[0].Operation)
		assert.Equal(t, cr1, data[0].Values["customResource"])
	})

	t.Run("custom-resource type in typesToStream subscribes to configuration webhook events", func(t *testing.T) {
		ctx := t.Context()

		// Provide a server so the async goroutine can call GetProject + GetConfiguration
		// without panicking on a nil ConsoleService.
		handler := func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/backend/projects/p1":
				json.NewEncoder(w).Encode(map[string]any{
					"_id":           "p1",
					"projectId":     "projectId",
					"name":          "name",
					"defaultBranch": "r1",
					"tenantId":      "",
					"info":          nil,
				})
			case "/backend/projects/p1/revisions/r1/configuration":
				// No custom-resource services — nothing emitted but no panic either.
				json.NewEncoder(w).Encode(map[string]any{
					"services": map[string]any{},
				})
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}

		server := httptest.NewServer(http.HandlerFunc(handler))
		defer server.Close()
		t.Setenv("CONSOLE_ENDPOINT", server.URL)

		cs, err := service.NewConsoleService()
		require.NoError(t, err)

		s := Source{
			cs: cs,
			c: &webhookClient{
				config: webhookConfig{
					WebhookPath:   "/webhook",
					WebhookSecret: "secret",
				},
			},
		}

		results := make(chan source.Data, 1)
		typesToStream := map[string]source.Extra{customResourceResource: {}}

		webhook, err := s.GetWebhook(ctx, typesToStream, results)
		require.NoError(t, err)
		require.NotNil(t, webhook.Handler)

		body, err := json.Marshal(map[string]any{
			"eventName": "configuration_created",
			"payload": map[string]any{
				"projectId":    "p1",
				"revisionName": "r1",
			},
		})
		require.NoError(t, err)

		headers := signedHeaders(body, "secret", "sha256=")
		// The handler dispatches async; it returns nil immediately.
		err = webhook.Handler(ctx, headers, body)
		require.NoError(t, err)

		// The configuration has no custom-resource services, so nothing is emitted.
		// Give the goroutine a moment to run and confirm the channel stays empty.
		waitCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()

		select {
		case <-results:
			t.Fatal("did not expect data in channel")
		case <-waitCtx.Done():
			// expected: goroutine completed, nothing emitted
		}
	})
}

func TestSource_processConfigurationServices_noRelevantTypes(t *testing.T) {
	t.Run("returns early when neither service nor custom-resource is requested", func(t *testing.T) {
		ctx := t.Context()

		// No server needed — the function must return before making any API call.
		s := Source{}

		ev := event{
			EventName:      "configuration_created",
			EventTimestamp: 1672531200000,
			Payload:        map[string]any{},
		}

		project := map[string]any{"_id": "p1", "defaultBranch": "r1"}
		ch := make(chan source.Data, 1)

		// types contains only projectResource — neither service nor custom-resource
		err := s.processConfigurationServices(ctx, project, "p1", "r1", ev, []string{projectResource}, ch)
		require.NoError(t, err)
		assert.Empty(t, ch)
	})
}

func TestSource_listConfigurations_customResourceNilDefaultBranch(t *testing.T) {
	t.Run("skips custom-resource when project defaultBranch is not a string", func(t *testing.T) {
		ctx := t.Context()

		// Project has no defaultBranch field at all — type assertion will fail.
		handler := func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/backend/projects/":
				json.NewEncoder(w).Encode([]map[string]any{{
					"_id":      "p1",
					"tenantId": "t1",
					// defaultBranch intentionally absent
				}})
			case "/backend/projects/p1/revisions":
				json.NewEncoder(w).Encode([]map[string]any{{"name": "r1"}})
			case "/backend/projects/p1/revisions/r1/configuration":
				json.NewEncoder(w).Encode(map[string]any{
					"services": map[string]any{
						"cr-1": map[string]any{"name": "cr-1", "type": "custom-resource"},
					},
				})
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}

		server := httptest.NewServer(http.HandlerFunc(handler))
		defer server.Close()
		t.Setenv("CONSOLE_ENDPOINT", server.URL)
		t.Setenv("CONSOLE_WEBHOOK_PATH", "/webhook")

		s, err := NewSource()
		require.NoError(t, err)

		data, err := s.listAssets(ctx, map[string]source.Extra{customResourceResource: {}})
		require.NoError(t, err)
		assert.Empty(t, data)
	})
}
