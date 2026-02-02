// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package mapper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/mapper"
)

func TestRelationshipMapping(t *testing.T) {
	t.Parallel()

	// Configuration as described by user
	mappings := map[string]string{
		"projectUniqueId": "{{ .project._id }}",
		"projectId":       `{{ get "projectId" .project "" | quote }}`,
		"tenantId":        "{{ .project.tenantId }}",
		"revisionName":    "{{ .revision.name }}",
	}

	// Extra mapping definition (mimicking parsed YAML)
	extraDef := map[string]any{
		"apiVersion": "resource.custom-platform/v1",
		"resource":   "relationships",
		"identifier": `{{ printf "relationship--%s--%s--dependency" .project._id .revision.name }}`,
		"sourceRef": map[string]any{
			"apiVersion": "resource.custom-platform/v1",
			"resource":   "Projects",
			"name":       "{{ .project._id }}",
		},
		"type": "dependency",
	}

	extras := []map[string]any{extraDef}

	identifierTemplate := `{{ printf "%s--%s" .project._id .revision.name }}`

	m, err := mapper.New(identifierTemplate, mappings, extras)
	require.NoError(t, err)

	// Sample Input Data
	input := map[string]any{
		"project": map[string]any{
			"_id":       "projectabc",
			"tenantId":  "tenant1",
			"projectId": "proj1",
		},
		"revision": map[string]any{
			"name": "main",
		},
	}

	// Execute
	output, extraOutputs, err := m.ApplyTemplates(input, mapper.ParentResourceInfo{
		ParentAPIVersion: "resource.custom-platform/v1",
		ParentResource:   "Configurations",
	})
	require.NoError(t, err)

	// Verify Main Output
	require.Equal(t, "projectabc--main", output.Identifier)
	require.Equal(t, "projectabc", output.Spec["projectUniqueId"])

	// Verify Extra Output
	require.Len(t, extraOutputs, 1)
	rel := extraOutputs[0]

	// Expected Identifier
	require.Equal(t, "relationship--projectabc--main--dependency", rel.Identifier)

	spec := rel.Spec
	require.Equal(t, "dependency", spec["type"])

	sourceRef, ok := spec["sourceRef"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "Projects", sourceRef["resource"]) // "resource" should be renamed to "resource"
	require.Equal(t, "projectabc", sourceRef["name"])

	targetRef, ok := spec["targetRef"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "Configurations", targetRef["resource"])
	require.Equal(t, "projectabc--main", targetRef["name"]) // Matches parent identifier
}
