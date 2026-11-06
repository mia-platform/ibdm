// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package jenkins

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/source"
)

func TestSyncInstance(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		body        string
		headers     map[string]string
		expectedVer string
		expectedMsg string
	}{
		"system message set": {
			body:        `{"_class": "hudson.model.Hudson", "description": "Builds for my-org.", "mode": "EXCLUSIVE"}`,
			headers:     map[string]string{versionHeader: "2.555.1"},
			expectedVer: "2.555.1",
			expectedMsg: "Builds for my-org.",
		},
		"system message not set": {
			body:        `{"_class": "hudson.model.Hudson", "description": null, "mode": "EXCLUSIVE"}`,
			headers:     map[string]string{versionHeader: "2.555.1"},
			expectedVer: "2.555.1",
		},
		"version header missing": {
			body:        `{"_class": "hudson.model.Hudson", "description": "Builds for my-org.", "mode": "EXCLUSIVE"}`,
			expectedMsg: "Builds for my-org.",
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			src := newTestSource(t, newFakeJenkins(map[string]fakeResponse{
				rootListingPath + "?tree=" + instanceTree: {body: tc.body, headers: tc.headers},
			}))

			results := make(chan source.Data, 1)
			require.NoError(t, src.syncInstance(t.Context(), results))
			close(results)

			data := collectData(t, results)
			require.Len(t, data, 1)
			assert.Equal(t, instanceType, data[0].Type)
			assert.Equal(t, source.DataOperationUpsert, data[0].Operation)
			assert.Equal(t, map[string]any{
				"instance": map[string]any{
					keyURL:        src.client.baseURL + "/",
					"version":     tc.expectedVer,
					"description": tc.expectedMsg,
					"mode":        "EXCLUSIVE",
				},
			}, data[0].Values)
		})
	}
}
