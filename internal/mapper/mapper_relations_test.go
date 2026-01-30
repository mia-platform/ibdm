// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package mapper_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
		"kind":       "Relationship",
		"identifier": `{{ printf "relationship--%s--%s--dependency" .project._id .revision.name }}`,
		"sourceRef": map[string]any{
			"apiVersion": "console.mia-platform.eu",
			"resource":   "Project",
			"name":       "{{ .project._id }}",
		},
		"type": "dependency",
	}

	extras := []map[string]any{extraDef}

	identifierTemplate := `{{ printf "%s--%s" .project._id .revision.name }}`

	m, err := mapper.New("console.mia-platform.eu/v1alpha1", "Configuration", identifierTemplate, mappings, extras)
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
	output, extraOutputs, err := m.ApplyTemplates(input)
	require.NoError(t, err)

	// Verify Main Output
	assert.Equal(t, "projectabc--main", output.Identifier)
	assert.Equal(t, "projectabc", output.Spec["projectUniqueId"])

	// Verify Extra Output
	require.Len(t, extraOutputs, 1)
	rel := extraOutputs[0]

	// Expected Identifier
	assert.Equal(t, "relationship--projectabc--main--dependency", rel.Identifier)

	// Expected Spec Structure
	// {
	//   "sourceRef": { "apiVersion": "...", "kind": "Project", "name": "projectabc" },
	//   "targetRef": { "apiVersion": "...", "kind": "Configuration", "name": "projectabc-main" },
	//   "type": "dependency"
	// }

	spec := rel.Spec
	assert.Equal(t, "dependency", spec["type"])

	sourceRef, ok := spec["sourceRef"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "Project", sourceRef["kind"]) // "resource" should be renamed to "kind"
	assert.Equal(t, "projectabc", sourceRef["name"])
	assert.Nil(t, sourceRef["resource"]) // should be removed

	targetRef, ok := spec["targetRef"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "Configuration", targetRef["kind"])
	assert.Equal(t, "projectabc--main", targetRef["name"]) // Matches parent identifier
}
