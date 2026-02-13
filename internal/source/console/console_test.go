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
				case "/projects/p1":
					json.NewEncoder(w).Encode(map[string]any{
						"_id":           "p1",
						"projectId":     "projectId",
						"name":          "name",
						"defaultBranch": "r1",
						"tenantId":      "",
					})
				case "/projects/p1/revisions/r1/configuration":
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
					Type:      revisionResource,
					Operation: source.DataOperationUpsert,
					Time:      time.Unix(1672531200, 0),
					Values: map[string]any{
						"project": map[string]any{
							"_id":       "p1",
							"projectId": "projectId",
							"name":      "name",
							"tenantId":  "",
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
			case "/projects/":
				projectResponse := maps.Clone(project1)
				projectResponse["defaultBranch"] = "r1"
				json.NewEncoder(w).Encode([]map[string]any{projectResponse})
			case "/projects/p1/revisions":
				json.NewEncoder(w).Encode([]map[string]any{revision1})
			case "/projects/p1/revisions/r1/configuration":
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
				if r.URL.Path == "/projects/" {
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
				case "/projects/":
					json.NewEncoder(w).Encode([]map[string]any{{"_id": "p1"}})
				case "/projects/p1/revisions":
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
