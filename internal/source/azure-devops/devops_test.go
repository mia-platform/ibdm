// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package azuredevops

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

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
