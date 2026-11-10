// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package jenkins

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodeDomains(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		raw         string
		expected    []credentialDomain
		expectError bool
	}{
		"missing": {raw: ``},
		"null":    {raw: `null`},
		"object keyed by name, ordered by name": {
			raw: `{"production": {"credentials": [{"id": "prod-key"}]}, "_": {"urlName": "_", "credentials": []}}`,
			expected: []credentialDomain{
				{URLName: "_", Credentials: []credentialEntry{}},
				{URLName: "production", Credentials: []credentialEntry{{ID: "prod-key"}}},
			},
		},
		"array": {
			raw:      `[{"urlName": "_", "credentials": [{"id": "my-key"}]}]`,
			expected: []credentialDomain{{URLName: "_", Credentials: []credentialEntry{{ID: "my-key"}}}},
		},
		"invalid object":  {raw: `{"_": "nope"}`, expectError: true},
		"invalid array":   {raw: `["nope"]`, expectError: true},
		"unexpected type": {raw: `"nope"`, expectError: true},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			domains, err := decodeDomains(json.RawMessage(tc.raw))
			if tc.expectError {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expected, domains)
		})
	}
}

func TestInvalidCredentialDomainsSkipTheStore(t *testing.T) {
	t.Parallel()

	routes := withRoutes(fixtureRoutes(), map[string]fakeResponse{
		myTeamStoreAPIPath: {body: `{"domains": "nope"}`},
	})
	data, _, err := runSync(t, routes, credentialType)
	require.NoError(t, err)
	require.Len(t, data, 1)
	assert.Equal(t, "deploy-key", data[0].Values["credential"].(map[string]any)["id"])
}

func TestResolveUsages(t *testing.T) {
	t.Parallel()

	rootParent := parentRef{kind: parentKindInstance, url: "https://ci.example.com/"}
	teamParent := parentRef{kind: parentKindFolder, fullName: "my-team", url: "https://ci.example.com/job/my-team/"}
	inv := &inventory{index: map[string]indexEntry{
		"my-team/my-service": {kind: usageKindWorkflow, fullName: "my-team/my-service", url: "https://ci.example.com/job/my-team/job/my-service/", parent: teamParent},
		"my-app":             {kind: usageKindMultibranch, fullName: "my-app", url: "https://ci.example.com/job/my-app/", parent: rootParent},
	}}

	usages := resolveUsages(t.Context(), inv, "my-key", []fingerprintUsage{
		{Name: "my-app/main"},
		{Name: "my-team/my-service"},
		{Name: "my-app/feature%2Flogin"},
		{Name: "my-team/my-service/nested"},
		{Name: "orphan"},
	})

	assert.Equal(t, []map[string]any{
		{
			keyKind: usageKindMultibranch, keyFullName: "my-app", keyURL: "https://ci.example.com/job/my-app/",
			"parent":   map[string]any{keyKind: parentKindInstance, keyFullName: "", keyURL: "https://ci.example.com/"},
			"branches": []string{"main", "feature%2Flogin"},
		},
		{
			keyKind: usageKindWorkflow, keyFullName: "my-team/my-service", keyURL: "https://ci.example.com/job/my-team/job/my-service/",
			"parent": map[string]any{keyKind: parentKindFolder, keyFullName: "my-team", keyURL: "https://ci.example.com/job/my-team/"},
		},
	}, usages)
}

func TestCredentialValuesStore(t *testing.T) {
	t.Parallel()

	credential := credentialEntry{ID: "my-key", DisplayName: "My key", TypeName: "Secret text", Description: "A key"}
	testCases := map[string]struct {
		store    storeRef
		expected map[string]any
	}{
		"system store": {
			store:    storeRef{kind: storeKindSystem, path: systemStorePath},
			expected: map[string]any{keyKind: storeKindSystem, "domain": "_"},
		},
		"folder store": {
			store: storeRef{kind: storeKindFolder, folder: &container{
				kind: containerKindFolder, segments: []string{"my-team"}, fullName: "my-team", url: "https://ci.example.com/job/my-team/",
			}},
			expected: map[string]any{
				keyKind: storeKindFolder, "domain": "_",
				"folder": map[string]any{keyKind: containerKindFolder, keyFullName: "my-team", keyURL: "https://ci.example.com/job/my-team/"},
			},
		},
		"multibranch store": {
			store: storeRef{kind: storeKindFolder, folder: &container{
				kind: containerKindMultibranch, segments: []string{"my-app"}, fullName: "my-app", url: "https://ci.example.com/job/my-app/",
			}},
			expected: map[string]any{
				keyKind: storeKindFolder, "domain": "_",
				"folder": map[string]any{keyKind: containerKindMultibranch, keyFullName: "my-app", keyURL: "https://ci.example.com/job/my-app/"},
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			values := credentialValues(t.Context(), &inventory{index: map[string]indexEntry{}}, tc.store, "_", credential)
			assert.Equal(t, tc.expected, values["store"])
		})
	}
}

func TestCutLast(t *testing.T) {
	t.Parallel()

	parent, name, found := cutLast("my-team/my-app/main")
	assert.True(t, found)
	assert.Equal(t, "my-team/my-app", parent)
	assert.Equal(t, "main", name)

	_, _, found = cutLast("root-pipeline")
	assert.False(t, found)
}
