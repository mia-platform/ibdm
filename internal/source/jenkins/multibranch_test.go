// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package jenkins

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrimaryBranchKeepsTheJobName(t *testing.T) {
	t.Parallel()

	routes := withRoutes(fixtureRoutes(), map[string]fakeResponse{
		myAppListingPath + "?tree=" + multibranchTree: {body: `{
  "jobs": [{
    "_class": "org.jenkinsci.plugins.workflow.job.WorkflowJob",
    "name": "release%2F1.0",
    "fullName": "my-team/my-app/release%2F1.0",
    "url": "https://ci.example.com/job/my-team/job/my-app/job/release%252F1.0/",
    "actions": [{"_class": "jenkins.scm.api.metadata.PrimaryInstanceMetadataAction"}]
  }],
  "views": []
}`},
		"/job/my-team/job/my-app/job/release%252F1.0" + apiJSONPath: {body: `{
  "_class": "org.jenkinsci.plugins.workflow.job.WorkflowJob",
  "name": "release%2F1.0",
  "fullName": "my-team/my-app/release%2F1.0",
  "displayName": "release/1.0",
  "url": "https://ci.example.com/job/my-team/job/my-app/job/release%252F1.0/",
  "buildable": true,
  "actions": [{"_class": "jenkins.scm.api.metadata.PrimaryInstanceMetadataAction"}],
  "lastCompletedBuild": null
}`},
	})

	data, _, err := runSync(t, routes, multibranchType, workflowType)
	require.NoError(t, err)

	var workflow, primary map[string]any
	for _, d := range data {
		switch d.Type {
		case workflowType:
			if values := d.Values["workflow"].(map[string]any); values[keyKind] == workflowKindPrimaryBranch {
				workflow = values
			}
		case multibranchType:
			if project := d.Values[multibranchValuesKey].(map[string]any); project[keyFullName] == "my-team/my-app" {
				primary, _ = project[primaryBranchValueKey].(map[string]any)
			}
		}
	}

	expected := map[string]any{
		keyName:     "release%2F1.0",
		keyFullName: "my-team/my-app/release%2F1.0",
		keyURL:      "https://ci.example.com/job/my-team/job/my-app/job/release%252F1.0/",
	}
	assert.Equal(t, expected, primary)
	require.NotNil(t, workflow)
	assert.Equal(t, expected[keyName], workflow[keyName])
	assert.Equal(t, expected[keyFullName], workflow[keyFullName])
	assert.Equal(t, expected[keyURL], workflow[keyURL])
}

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
