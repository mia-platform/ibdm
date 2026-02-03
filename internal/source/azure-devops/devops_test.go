// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package azuredevops

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/source"
)

func TestNewSource(t *testing.T) {
	testCases := map[string]struct {
		setupEnv       func(t *testing.T)
		expectedConfig config
	}{
		"with all env": {
			setupEnv: func(t *testing.T) {
				t.Helper()
				t.Setenv("AZURE_DEVOPS_ORGANIZATION_URL", "https://dev.azure.com/myorg")
				t.Setenv("AZURE_DEVOPS_PERSONAL_TOKEN", "my-token")
			},
			expectedConfig: config{
				OrganizationURL: "https://dev.azure.com/myorg",
				PersonalToken:   "my-token",
				WebhookPath:     "/azure-devops/webhook",
			},
		},
		"with missing env": {
			setupEnv: func(t *testing.T) {
				t.Helper()
				t.Setenv("AZURE_DEVOPS_PERSONAL_TOKEN", "my-token")
			},
			expectedConfig: config{
				OrganizationURL: "",
				PersonalToken:   "my-token",
				WebhookPath:     "/azure-devops/webhook",
			},
		},
		"webhook configs": {
			setupEnv: func(t *testing.T) {
				t.Helper()
				t.Setenv("AZURE_DEVOPS_WEBHOOK_PATH", "/custom/path")
				t.Setenv("AZURE_DEVOPS_WEBHOOK_USER", "user")
				t.Setenv("AZURE_DEVOPS_WEBHOOK_PASSWORD", "password")
			},
			expectedConfig: config{
				OrganizationURL: "",
				PersonalToken:   "",
				WebhookPath:     "/custom/path",
				WebhookUser:     "user",
				WebhookPassword: "password",
			},
		},
	}

	for testName, test := range testCases {
		t.Run(testName, func(t *testing.T) {
			test.setupEnv(t)
			source, err := NewSource()
			assert.NoError(t, err)
			assert.NotNil(t, source)
			assert.Equal(t, test.expectedConfig, source.config)
		})
	}
}

func TestStartSyncProcess(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct{}{}

	for testName, test := range testCases {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()
			_ = test
		})
	}
}

func TestMultipleSyncStart(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)

	syncChan := make(chan struct{})
	hangedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			defer r.Body.Close()
		}

		syncChan <- struct{}{}
		<-ctx.Done()
		http.NotFound(w, r)
		close(syncChan)
	}))
	defer hangedServer.Close()

	src := &Source{
		config: config{
			OrganizationURL: hangedServer.URL,
			PersonalToken:   "dummy-token",
		},
	}

	typesToFilter := map[string]source.Extra{
		gitRepositoryType: {},
	}
	go func() {
		err := src.StartSyncProcess(t.Context(), typesToFilter, nil)
		assert.NoError(t, err)
	}()

	<-syncChan
	err := src.StartSyncProcess(t.Context(), nil, nil)
	assert.NoError(t, err)
	src.Close(ctx, 1*time.Second)

	cancel()
	<-syncChan
	assert.ErrorIs(t, ctx.Err(), context.Canceled)
}

func TestWebhookHandler(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		config        config
		typesToStream map[string]source.Extra
		headers       http.Header
		body          string
		expectedData  source.Data
		expectedError error
	}{
		"git.repository.create": {
			config: config{
				WebhookPath:     "/azure-devops/webhook",
				WebhookUser:     "user",
				WebhookPassword: "password",
			},
			typesToStream: map[string]source.Extra{
				"gitrepository": {
					"eventNames": []string{"git.repo.created"},
				},
			},
			body: repoCreatedPayload,
			headers: http.Header{
				"Authorization": []string{"Basic " + base64.StdEncoding.EncodeToString([]byte("user:password"))},
			},
			expectedData: source.Data{
				Type:      "gitrepository",
				Operation: source.DataOperationUpsert,
				Time:      time.Date(2022, 12, 12, 12, 34, 56, 549845900, time.UTC),
				Values: map[string]any{
					"id":   "278d5cd2-584d-4b63-824a-2ba458937249",
					"name": "Fabrikam-Fiber-Git",
					"url":  "https://fabrikam-fiber-inc.visualstudio.com/DefaultCollection/_apis/git/repositories/278d5cd2-584d-4b63-824a-2ba458937249",
					"project": map[string]any{
						"id":             "6ce954b1-ce1f-45d1-b94d-e6bf2464ba2c",
						"name":           "Fabrikam-Fiber-Git",
						"url":            "https://fabrikam-fiber-inc.visualstudio.com/DefaultCollection/_apis/projects/6ce954b1-ce1f-45d1-b94d-e6bf2464ba2c",
						"state":          "wellFormed",
						"revision":       float64(11),
						"visibility":     "private",
						"lastUpdateTime": "2026-02-02T11:47:33.561308+00:00",
					},
					"defaultBranch": "refs/heads/master",
					"size":          float64(728),
					"remoteUrl":     "https://fabrikam-fiber-inc.visualstudio.com/DefaultCollection/_git/Fabrikam-Fiber-Git",
					"sshUrl":        "ssh://git@ssh.fabrikam-fiber-inc.visualstudio.com/v3/DefaultCollection/Fabrikam-Fiber-Git",
					"isDisabled":    false,
				},
			},
		},
		"git.repository.rename": {
			config: config{
				WebhookPath: "/azure-devops/webhook",
			},
			typesToStream: map[string]source.Extra{
				"gitrepository": {
					"eventNames": []string{"git.repo.renamed"},
				},
			},
			body: repoRenamedPayload,
			expectedData: source.Data{
				Type:      "gitrepository",
				Operation: source.DataOperationUpsert,
				Time:      time.Date(2022, 12, 12, 12, 34, 56, 549845900, time.UTC),
				Values: map[string]any{
					"id":   "278d5cd2-584d-4b63-824a-2ba458937249",
					"name": "Fabrikam-Fiber-Git",
					"url":  "https://fabrikam-fiber-inc.visualstudio.com/DefaultCollection/_apis/git/repositories/278d5cd2-584d-4b63-824a-2ba458937249",
					"project": map[string]any{
						"id":             "6ce954b1-ce1f-45d1-b94d-e6bf2464ba2c",
						"name":           "Contoso",
						"url":            "https://fabrikam-fiber-inc.visualstudio.com/DefaultCollection/_apis/projects/6ce954b1-ce1f-45d1-b94d-e6bf2464ba2c",
						"state":          "wellFormed",
						"revision":       float64(11),
						"visibility":     "private",
						"lastUpdateTime": "2026-02-02T13:09:14.238036+00:00",
					},
					"defaultBranch": "refs/heads/master",
					"size":          float64(728),
					"remoteUrl":     "https://fabrikam-fiber-inc.visualstudio.com/DefaultCollection/_git/Fabrikam-Fiber-Git",
					"sshUrl":        "ssh://git@ssh.fabrikam-fiber-inc.visualstudio.com/v3/DefaultCollection/Fabrikam-Fiber-Git",
					"isDisabled":    false,
				},
			},
		},
		"git.repository.deleted": {
			config: config{
				WebhookPath: "/azure-devops/webhook",
			},
			typesToStream: map[string]source.Extra{
				"gitrepository": {
					"eventNames": []string{"git.repo.deleted"},
				},
			},
			body: repoDeletedPayload,
			expectedData: source.Data{
				Type:      "gitrepository",
				Operation: source.DataOperationDelete,
				Time:      time.Date(2022, 12, 12, 12, 34, 56, 549845900, time.UTC),
				Values: map[string]any{
					"project": map[string]any{
						"id":             "6ce954b1-ce1f-45d1-b94d-e6bf2464ba2c",
						"name":           "Contoso",
						"url":            "https://fabrikam-fiber-inc.visualstudio.com/DefaultCollection/_apis/projects/6ce954b1-ce1f-45d1-b94d-e6bf2464ba2c",
						"state":          "wellFormed",
						"revision":       float64(11),
						"visibility":     "private",
						"lastUpdateTime": "2026-02-02T11:52:41.2405443+00:00",
					},
					"repositoryId":   "278d5cd2-584d-4b63-824a-2ba458937249",
					"repositoryName": "Fabrikam-Fiber-Git",
					"isHardDelete":   false,
					"initiatedBy": map[string]any{
						"displayName": "John Johnson",
						"id":          "c6917b69-506a-6a69-81a5-81a7fed0bff9",
						"uniqueName":  "user@fabrikamfiber.com",
					},
					"utcTimestamp": "2022-12-12T12:34:56.5498459Z",
				},
			},
		},
	}

	for testName, test := range testCases {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
			defer cancel()

			src := &Source{
				config: test.config,
			}
			dataChannel := make(chan source.Data)
			defer close(dataChannel)

			webhook, err := src.GetWebhook(t.Context(), test.typesToStream, dataChannel)
			require.NoError(t, err)

			assert.Equal(t, http.MethodPost, webhook.Method)
			assert.Equal(t, test.config.WebhookPath, webhook.Path)

			err = webhook.Handler(ctx, test.headers, []byte(test.body))
			if test.expectedError != nil {
				assert.ErrorIs(t, err, test.expectedError)
				return
			}

			select {
			case data := <-dataChannel:
				assert.Equal(t, test.expectedData, data)
			case <-ctx.Done():
				assert.Fail(t, "timeout waiting for data")
			}
		})
	}
}

const (
	repoCreatedPayload = `{
	"subscriptionId": "8ec9745b-5b8a-4e19-a538-90693987c736",
	"notificationId": 6,
	"id": "03c164c2-8912-4d5e-8009-3707d5f83734",
	"eventType": "git.repo.created",
	"publisherId": "tfs",
	"message": null,
	"detailedMessage": null,
	"resource": {
		"repository": {
			"id": "278d5cd2-584d-4b63-824a-2ba458937249",
			"name": "Fabrikam-Fiber-Git",
			"url": "https://fabrikam-fiber-inc.visualstudio.com/DefaultCollection/_apis/git/repositories/278d5cd2-584d-4b63-824a-2ba458937249",
			"project": {
				"id": "6ce954b1-ce1f-45d1-b94d-e6bf2464ba2c",
				"name": "Fabrikam-Fiber-Git",
				"url": "https://fabrikam-fiber-inc.visualstudio.com/DefaultCollection/_apis/projects/6ce954b1-ce1f-45d1-b94d-e6bf2464ba2c",
				"state": "wellFormed",
				"revision": 11,
				"visibility": "private",
				"lastUpdateTime": "2026-02-02T11:47:33.561308+00:00"
			},
			"defaultBranch": "refs/heads/master",
			"size": 728,
			"remoteUrl": "https://fabrikam-fiber-inc.visualstudio.com/DefaultCollection/_git/Fabrikam-Fiber-Git",
			"sshUrl": "ssh://git@ssh.fabrikam-fiber-inc.visualstudio.com/v3/DefaultCollection/Fabrikam-Fiber-Git",
			"isDisabled": false
		},
		"initiatedBy": {
			"displayName": "Ivan Yurev",
			"id": "c6917b69-506a-6a69-81a5-81a7fed0bff9",
			"uniqueName": "user@fabrikamfiber.com"
		},
		"utcTimestamp": "2022-12-12T12:34:56.5498459Z"
	},
	"resourceVersion": "1.0-preview.1",
	"resourceContainers": {
		"collection": {
			"id": "c12d0eb8-e382-443b-9f9c-c52cba5014c2"
		},
		"account": {
			"id": "f844ec47-a9db-4511-8281-8b63f4eaf94e"
		},
		"project": {
			"id": "be9b3917-87e6-42a4-a549-2bc06a7a878f"
		}
	},
	"createdDate": "2026-02-02T11:47:33.9085576Z"
}`

	repoRenamedPayload = `{
	"subscriptionId": "8ec9745b-5b8a-4e19-a538-90693987c736",
	"notificationId": 10,
	"id": "03c164c2-8912-4d5e-8009-3707d5f83734",
	"eventType": "git.repo.renamed",
	"publisherId": "tfs",
	"message": null,
	"detailedMessage": null,
	"resource": {
		"oldName": "Diber-Git",
		"newName": "Fabrikam-Fiber-Git",
		"repository": {
			"id": "278d5cd2-584d-4b63-824a-2ba458937249",
			"name": "Fabrikam-Fiber-Git",
			"url": "https://fabrikam-fiber-inc.visualstudio.com/DefaultCollection/_apis/git/repositories/278d5cd2-584d-4b63-824a-2ba458937249",
			"project": {
				"id": "6ce954b1-ce1f-45d1-b94d-e6bf2464ba2c",
				"name": "Contoso",
				"url": "https://fabrikam-fiber-inc.visualstudio.com/DefaultCollection/_apis/projects/6ce954b1-ce1f-45d1-b94d-e6bf2464ba2c",
				"state": "wellFormed",
				"revision": 11,
				"visibility": "private",
				"lastUpdateTime": "2026-02-02T13:09:14.238036+00:00"
			},
			"defaultBranch": "refs/heads/master",
			"size": 728,
			"remoteUrl": "https://fabrikam-fiber-inc.visualstudio.com/DefaultCollection/_git/Fabrikam-Fiber-Git",
			"sshUrl": "ssh://git@ssh.fabrikam-fiber-inc.visualstudio.com/v3/DefaultCollection/Fabrikam-Fiber-Git",
			"isDisabled": false
		},
		"initiatedBy": {
			"displayName": "John Johnson",
			"id": "c6917b69-506a-6a69-81a5-81a7fed0bff9",
			"uniqueName": "user@fabrikamfiber.com"
		},
		"utcTimestamp": "2022-12-12T12:34:56.5498459Z"
	},
	"resourceVersion": "1.0-preview.1",
	"resourceContainers": {
		"collection": {
			"id": "c12d0eb8-e382-443b-9f9c-c52cba5014c2"
		},
		"account": {
			"id": "f844ec47-a9db-4511-8281-8b63f4eaf94e"
		},
		"project": {
			"id": "be9b3917-87e6-42a4-a549-2bc06a7a878f"
		}
	},
	"createdDate": "2026-02-02T13:09:14.6837777Z"
}`

	repoDeletedPayload = `{
	"subscriptionId": "8ec9745b-5b8a-4e19-a538-90693987c736",
	"notificationId": 9,
	"id": "03c164c2-8912-4d5e-8009-3707d5f83734",
	"eventType": "git.repo.deleted",
	"publisherId": "tfs",
	"message": null,
	"detailedMessage": null,
	"resource": {
		"project": {
			"id": "6ce954b1-ce1f-45d1-b94d-e6bf2464ba2c",
			"name": "Contoso",
			"url": "https://fabrikam-fiber-inc.visualstudio.com/DefaultCollection/_apis/projects/6ce954b1-ce1f-45d1-b94d-e6bf2464ba2c",
			"state": "wellFormed",
			"revision": 11,
			"visibility": "private",
			"lastUpdateTime": "2026-02-02T11:52:41.2405443+00:00"
		},
		"repositoryId": "278d5cd2-584d-4b63-824a-2ba458937249",
		"repositoryName": "Fabrikam-Fiber-Git",
		"isHardDelete": false,
		"initiatedBy": {
			"displayName": "John Johnson",
			"id": "c6917b69-506a-6a69-81a5-81a7fed0bff9",
			"uniqueName": "user@fabrikamfiber.com"
		},
		"utcTimestamp": "2022-12-12T12:34:56.5498459Z"
	},
	"resourceVersion": "1.0-preview.1",
	"resourceContainers": {
		"collection": {
			"id": "c12d0eb8-e382-443b-9f9c-c52cba5014c2"
		},
		"account": {
			"id": "f844ec47-a9db-4511-8281-8b63f4eaf94e"
		},
		"project": {
			"id": "be9b3917-87e6-42a4-a549-2bc06a7a878f"
		}
	},
	"createdDate": "2026-02-02T11:52:41.2982716Z"
}`
)
