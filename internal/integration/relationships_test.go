// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

//go:build integration

package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const urnPrefix = "urn:mia-platform-catalog:"

// itemKinds binds the item families of the internal mappings to the kind their URNs carry. The
// families shared by several integrations carry the same kind in all of them.
var itemKinds = map[string]string{
	"accesstokens":    "AccessToken",
	"clusters":        "Cluster",
	"customresources": "CustomResource",
	"dockerimages":    "DockerImage",
	"issues":          "Issue",
	"pipelines":       "Pipeline",
	"projects":        "Project",
	"repositories":    "Repository",
	"revisions":       "Revision",
	"runs":            "Run",
	"services":        "Service",
	"vulnerabilities": "Vulnerability",
	"workflowruns":    "WorkflowRun",
}

// itemURN builds the URN a relationship uses to point at item.
func itemURN(t *testing.T, item map[string]any) (string, bool) {
	t.Helper()

	kind, ok := itemKinds[item["itemFamily"].(string)]
	if !ok {
		return "", false
	}
	apiVersion, ok := item["apiVersion"].(string)
	require.True(t, ok)
	group, version, found := strings.Cut(apiVersion, "/")
	require.True(t, found, "apiVersion %q has no version", apiVersion)
	return urnPrefix + group + ":" + version + ":" + kind + ":" + item["name"].(string), true
}

// assertRelationshipsResolve checks that both ends of every relationship are items of the same
// run, except the URNs under one of the external groups, which other integrations write.
func assertRelationshipsResolve(t *testing.T, items []map[string]any, externalGroups ...string) {
	t.Helper()

	written := make(map[string]bool)
	for _, item := range items {
		if urn, ok := itemURN(t, item); ok {
			written[urn] = true
		}
	}

	for _, relationship := range itemsOf(items, relationshipsAPIVersion, "relationships") {
		spec, ok := relationship["data"].(map[string]any)
		require.True(t, ok)
		for _, end := range []string{"sourceRef", "targetRef"} {
			ref, ok := spec[end].(string)
			require.True(t, ok, "relationship %v has no %s", relationship["name"], end)
			if isExternal(ref, externalGroups) {
				continue
			}
			assert.True(t, written[ref], "the %s of relationship %v points at an item the run did not write: %s", end, relationship["name"], ref)
		}
	}
}

// isExternal reports whether ref is a URN of one of groups.
func isExternal(ref string, groups []string) bool {
	for _, group := range groups {
		if strings.HasPrefix(ref, urnPrefix+group+":") {
			return true
		}
	}
	return false
}
