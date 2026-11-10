// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package jenkins

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLibraryCollectorValues(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		records           []libraryRecord
		expectedVersions  []string
		expectedVariables []string
		expectedJSON      string
	}{
		"library without global variables": {
			records:           []libraryRecord{{Name: "my-lib", Version: "main"}},
			expectedVersions:  []string{"main"},
			expectedVariables: []string{},
			expectedJSON:      `{"name":"my-lib","variables":[],"versions":["main"]}`,
		},
		"versions and variables are sorted and deduplicated": {
			records: []libraryRecord{
				{Name: "my-lib", Version: "v2.0.0", Variables: []string{"deployApp", "buildApp"}},
				{Name: "my-lib", Version: "main", Variables: []string{"buildApp"}},
				{Name: "my-lib", Version: "v2.0.0"},
			},
			expectedVersions:  []string{"main", "v2.0.0"},
			expectedVariables: []string{"buildApp", "deployApp"},
			expectedJSON:      `{"name":"my-lib","variables":["buildApp","deployApp"],"versions":["main","v2.0.0"]}`,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			collector := newLibraryCollector()
			for idx, record := range tc.records {
				workflow := jobItem{Name: "my-service", FullName: "my-team/my-service", URL: "https://ci.example.com/job/my-team/job/my-service/"}
				build := &buildInfo{Number: int64(idx + 1), Actions: []buildAction{{Libraries: []libraryRecord{record}}}}
				collector.add(workflow, workflowKindStandalone, nil, build)
			}

			values := collector.values()
			require.Len(t, values, 1)
			library := values[0]["library"].(map[string]any)
			assert.Equal(t, tc.expectedVersions, library["versions"])
			assert.Equal(t, tc.expectedVariables, library["variables"])
			assert.Len(t, values[0]["usages"], len(tc.records))

			encoded, err := json.Marshal(library)
			require.NoError(t, err)
			assert.JSONEq(t, tc.expectedJSON, string(encoded))
		})
	}
}

func TestSortedSet(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{}, sortedSet(nil))
	assert.NotNil(t, sortedSet(map[string]struct{}{}))
	assert.Equal(t, []string{"a", "b", "c"}, sortedSet(map[string]struct{}{"c": {}, "a": {}, "b": {}}))
}
