// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package gitlab

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/mapper"
	"github.com/mia-platform/ibdm/internal/mappings"
)

const (
	// internalMappingsSource is the source the internal GitLab mappings are bundled for.
	internalMappingsSource = "gitlab"
	// projectsMappingName is the name of the internal projects mapping.
	projectsMappingName = "projects"
)

// testProjectJSON is a project as the GitLab REST API returns it.
const testProjectJSON = `{
  "id": 102,
  "name": "legacy-portal",
  "name_with_namespace": "Acme Widgets / legacy-portal",
  "path": "legacy-portal",
  "path_with_namespace": "acme-widgets/legacy-portal",
  "created_at": "2025-02-01T09:00:00Z",
  "default_branch": "main",
  "tag_list": ["catalog"],
  "topics": ["catalog"],
  "ssh_url_to_repo": "git@git.example.com:acme-widgets/legacy-portal.git",
  "http_url_to_repo": "https://git.example.com/acme-widgets/legacy-portal.git",
  "web_url": "https://git.example.com/acme-widgets/legacy-portal",
  "forks_count": 1,
  "star_count": 4,
  "last_activity_at": "2025-12-01T08:00:00Z",
  "packages_enabled": true,
  "empty_repo": false,
  "archived": true,
  "visibility": "private",
  "issues_enabled": true,
  "merge_requests_enabled": true,
  "wiki_enabled": false,
  "jobs_enabled": true,
  "snippets_enabled": false,
  "container_registry_enabled": true,
  "service_desk_enabled": false,
  "can_create_merge_request_in": true,
  "issues_access_level": "enabled",
  "repository_access_level": "enabled",
  "merge_requests_access_level": "enabled",
  "forking_access_level": "enabled",
  "wiki_access_level": "disabled",
  "builds_access_level": "enabled",
  "snippets_access_level": "disabled",
  "pages_access_level": "private",
  "security_and_compliance_access_level": "private",
  "releases_access_level": "enabled",
  "environments_access_level": "enabled",
  "feature_flags_access_level": "enabled",
  "infrastructure_access_level": "enabled",
  "package_registry_access_level": "enabled",
  "creator_id": 42,
  "public_jobs": true,
  "only_allow_merge_if_pipeline_succeeds": true,
  "request_access_enabled": false,
  "merge_method": "merge",
  "owner": null,
  "updated_at": "2025-12-01T08:00:00Z"
}`

// testProject decodes testProjectJSON with encoding/json, as the client does, so numbers are
// float64 and JSON nulls are nil values.
func testProject(t *testing.T) map[string]any {
	t.Helper()

	var project map[string]any
	require.NoError(t, json.Unmarshal([]byte(testProjectJSON), &project))
	return project
}

// loadInternalMapper builds the mapper of the internal GitLab mapping called name the way the CLI
// does, from the mappings bundled in the binary, and checks that it maps resourceType.
func loadInternalMapper(t *testing.T, name, resourceType string) mapper.Mapper {
	t.Helper()

	configs, err := mappings.ForSource(internalMappingsSource)
	require.NoError(t, err)

	for _, mapping := range configs {
		if mapping.Name != name {
			continue
		}

		require.Equal(t, resourceType, mapping.Type)
		templates := mapping.Mappings
		m, err := mapper.New(templates.Identifier, templates.Metadata, templates.Spec, templates.Extra, mapper.WithCreateIf(mapping.CreateIf))
		require.NoError(t, err)
		return m
	}

	require.FailNow(t, "internal gitlab mapping not found", name)
	return nil
}

// TestProjectsMappingNullableFields renders the internal projects mapping over the values the
// sync and the webhooks emit, for the project fields GitLab can return as null or omit.
func TestProjectsMappingNullableFields(t *testing.T) {
	t.Parallel()

	m := loadInternalMapper(t, projectsMappingName, projectResource)

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

			project := testProject(t)
			for key, value := range testCase.changes {
				project[key] = value
			}
			for _, key := range testCase.removed {
				delete(project, key)
			}

			output, _, err := m.ApplyTemplates(projectWrapper(project, map[string]any{}), mapper.ParentItemInfo{})
			require.NoError(t, err)

			for field, expected := range testCase.expected {
				require.Equal(t, expected, output.Spec[field], field)
			}
		})
	}
}
