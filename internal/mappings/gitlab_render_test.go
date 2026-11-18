// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package mappings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/mapper"
)

const (
	gitlabSource         = "gitlab"
	gitlabFixturesDir    = "testdata/gitlab"
	gitlabProjectFixture = "project.json"
)

// loadGitLabMapper builds the mapper of the bundled GitLab mapping called name, like the CLI does.
func loadGitLabMapper(t *testing.T, name string) mapper.Mapper {
	t.Helper()

	configs, err := ForSource(gitlabSource)
	require.NoError(t, err)

	for _, mapping := range configs {
		if mapping.Name != name {
			continue
		}

		templates := mapping.Mappings
		m, err := mapper.New(templates.Identifier, templates.Metadata, templates.Spec, templates.Extra, mapper.WithCreateIf(mapping.CreateIf))
		require.NoError(t, err)
		return m
	}

	require.FailNow(t, "bundled gitlab mapping not found", name)
	return nil
}

// loadGitLabProject decodes a GitLab REST API project fixture with encoding/json, as the source
// does, so numbers are float64 and JSON nulls are nil values.
func loadGitLabProject(t *testing.T) map[string]any {
	t.Helper()

	content, err := os.ReadFile(filepath.Join(gitlabFixturesDir, gitlabProjectFixture))
	require.NoError(t, err)

	var project map[string]any
	require.NoError(t, json.Unmarshal(content, &project))
	return project
}

func TestGitLabProjectMappingNullableFields(t *testing.T) {
	t.Parallel()

	m := loadGitLabMapper(t, "projects")

	testCases := map[string]struct {
		changes  map[string]any
		removed  []string
		expected map[string]any
	}{
		"values are kept as GitLab returns them": {
			expected: map[string]any{"creatorId": 42, "packagesEnabled": true, "defaultBranch": "main"},
		},
		"an explicit false packages_enabled wins over the registry access level": {
			changes:  map[string]any{"packages_enabled": false},
			expected: map[string]any{"creatorId": 42, "packagesEnabled": false, "defaultBranch": "main"},
		},
		"null values fall back, packages from an enabled registry access level": {
			changes:  map[string]any{"creator_id": nil, "packages_enabled": nil, "default_branch": nil},
			expected: map[string]any{"creatorId": 0, "packagesEnabled": true, "defaultBranch": ""},
		},
		"a null packages_enabled with a disabled registry access level": {
			changes:  map[string]any{"packages_enabled": nil, "package_registry_access_level": "disabled"},
			expected: map[string]any{"creatorId": 42, "packagesEnabled": false, "defaultBranch": "main"},
		},
		"a null packages_enabled with a null registry access level": {
			changes:  map[string]any{"packages_enabled": nil, "package_registry_access_level": nil},
			expected: map[string]any{"creatorId": 42, "packagesEnabled": false, "defaultBranch": "main"},
		},
		"missing keys fall back": {
			removed:  []string{"creator_id", "packages_enabled", "package_registry_access_level", "default_branch"},
			expected: map[string]any{"creatorId": 0, "packagesEnabled": false, "defaultBranch": ""},
		},
		"a branch name that reads as another YAML type stays a string": {
			changes:  map[string]any{"default_branch": "1.0"},
			expected: map[string]any{"creatorId": 42, "packagesEnabled": true, "defaultBranch": "1.0"},
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			project := loadGitLabProject(t)
			for key, value := range testCase.changes {
				project[key] = value
			}
			for _, key := range testCase.removed {
				delete(project, key)
			}

			output, _, err := m.ApplyTemplates(map[string]any{
				"project":           project,
				"project_languages": map[string]any{},
			}, mapper.ParentItemInfo{})
			require.NoError(t, err)

			for field, expected := range testCase.expected {
				require.Equal(t, expected, output.Spec[field], field)
			}
		})
	}
}
