// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package jenkins

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMultibranchFailures(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		overrides         map[string]fakeResponse
		expectedWorkflows []string
		expectPrimary     bool
		expectCounts      bool
	}{
		"branches cannot be listed": {
			overrides:         map[string]fakeResponse{myAppListingPath + "?tree=" + multibranchTree: failingResponse},
			expectedWorkflows: []string{"my-team/my-service", "root-pipeline"},
		},
		"primary branch cannot be read": {
			overrides:         map[string]fakeResponse{myAppMainPath: failingResponse},
			expectedWorkflows: []string{"my-team/my-service", "root-pipeline"},
			expectPrimary:     true,
			expectCounts:      true,
		},
		"no primary branch": {
			overrides: map[string]fakeResponse{
				myAppListingPath + "?tree=" + multibranchTree: {body: myRepoListingFixture},
			},
			expectedWorkflows: []string{"my-team/my-service", "root-pipeline"},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			data, _, err := runSync(t, withRoutes(fixtureRoutes(), tc.overrides), multibranchType, workflowType)
			require.NoError(t, err)

			grouped := byType(t, data)
			workflows := make([]string, 0, len(grouped[workflowType]))
			for _, d := range grouped[workflowType] {
				workflows = append(workflows, d.Values["workflow"].(map[string]any)[keyFullName].(string))
			}
			assert.Equal(t, tc.expectedWorkflows, workflows)

			require.Len(t, grouped[multibranchType], 2)
			project := grouped[multibranchType][0].Values["multibranch"].(map[string]any)
			assert.Equal(t, "my-team/my-app", project[keyFullName])
			assert.Equal(t, tc.expectPrimary, project[primaryBranchValueKey] != nil)
			_, hasCounts := project[branchCountKey]
			assert.Equal(t, tc.expectCounts, hasCounts)
		})
	}
}
