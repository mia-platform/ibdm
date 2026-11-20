// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package jenkins

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/mapper"
	"github.com/mia-platform/ibdm/internal/mappings"
	"github.com/mia-platform/ibdm/internal/source"
)

// rendered holds what the built-in mappings render over one sync, keyed by data type: the item
// identifiers, and the identifier, sourceRef and targetRef of the relationships.
type rendered map[string][]string

// builtInMappers returns a mapper for every built-in Jenkins mapping, keyed by data type.
func builtInMappers(t *testing.T) map[string]mapper.Mapper {
	t.Helper()

	configs, err := mappings.ForSource("jenkins")
	require.NoError(t, err)

	mappers := make(map[string]mapper.Mapper, len(configs))
	for _, mapping := range configs {
		m, err := mapper.New(mapping.Mappings.Identifier, mapping.Mappings.Metadata, mapping.Mappings.Spec, mapping.Mappings.Extra, mapper.WithCreateIf(mapping.CreateIf))
		require.NoError(t, err, mapping.Name)
		mappers[mapping.Type] = m
	}
	require.ElementsMatch(t, slices.Collect(maps.Keys(knownTypes)), slices.Collect(maps.Keys(mappers)), "one built-in mapping per data type")
	return mappers
}

// renderSync runs a full sync of the fixture controller configured with configuredURL, and
// renders every emitted value with the built-in mappings.
func renderSync(t *testing.T, mappers map[string]mapper.Mapper, configuredURL string) rendered {
	t.Helper()

	src := newTestSource(t, newFakeJenkins(fixtureRoutes()))
	controller, err := newControllerRef(configuredURL)
	require.NoError(t, err)
	src.controller = controller

	typesToSync := make(map[string]source.MappingExtras, len(knownTypes))
	for dataType := range knownTypes {
		typesToSync[dataType] = source.MappingExtras{}
	}
	results := make(chan source.Data, 100)
	require.NoError(t, src.StartSyncProcess(t.Context(), typesToSync, results))
	close(results)

	out := make(rendered)
	for _, data := range collectData(t, results) {
		output, extras, err := mappers[data.Type].ApplyTemplates(data.Values, mapper.ParentItemInfo{})
		require.NoError(t, err, data.Type)

		out[data.Type] = append(out[data.Type], output.Identifier)
		for _, extra := range extras {
			out[data.Type+" relationship"] = append(out[data.Type+" relationship"], extra.Identifier)
			out[data.Type+" sourceRef"] = append(out[data.Type+" sourceRef"], extra.Spec["sourceRef"].(string))
			out[data.Type+" targetRef"] = append(out[data.Type+" targetRef"], extra.Spec["targetRef"].(string))
		}
	}

	for key := range out {
		slices.Sort(out[key])
	}
	return out
}

// TestBuiltInMappingsIdentifyTheController renders the built-in mappings over the same Jenkins
// data read through different controller URLs: the identifiers depend on the normalised
// configured URL only.
func TestBuiltInMappingsIdentifyTheController(t *testing.T) {
	t.Parallel()

	mappers := builtInMappers(t)
	reference := renderSync(t, mappers, "https://ci.example.com/jenkins")
	for dataType := range knownTypes {
		require.NotEmpty(t, reference[dataType], "the fixtures emit every data type: %s", dataType)
	}
	for _, dataType := range []string{folderType, multibranchType, workflowType, pluginType} {
		require.NotEmpty(t, reference[dataType+" relationship"], "the fixtures emit relationships of %s", dataType)
	}

	t.Run("relationships point at the items of the same controller", func(t *testing.T) {
		t.Parallel()

		identifiers := make(map[string]struct{})
		for dataType := range knownTypes {
			for _, identifier := range reference[dataType] {
				identifiers[identifier] = struct{}{}
			}
		}
		for key, refs := range reference {
			if !strings.HasSuffix(key, "Ref") {
				continue
			}
			for _, ref := range refs {
				identifier := ref[strings.LastIndex(ref, ":")+1:]
				assert.Contains(t, identifiers, identifier, "%s %s", key, ref)
			}
		}
	})

	equivalent := []string{
		"https://CI.example.com:443/jenkins/",
		"https://ci.example.com:443/jenkins",
		"http://ci.example.com/jenkins",
		"HTTP://ci.EXAMPLE.com:80/jenkins/",
	}
	for _, configuredURL := range equivalent {
		t.Run("same identifiers through "+configuredURL, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, reference, renderSync(t, mappers, configuredURL))
		})
	}

	distinct := []string{
		"https://ci.example.com/other",
		"https://ci.example.com",
		"https://ci.example.com:8443/jenkins",
		"https://ci-2.example.com/jenkins",
	}
	for _, configuredURL := range distinct {
		t.Run("distinct identifiers through "+configuredURL, func(t *testing.T) {
			t.Parallel()

			other := renderSync(t, mappers, configuredURL)
			require.ElementsMatch(t, slices.Collect(maps.Keys(reference)), slices.Collect(maps.Keys(other)))
			for key, values := range reference {
				require.Len(t, other[key], len(values), key)
				for _, value := range other[key] {
					assert.NotContains(t, values, value, "%s is shared by two controllers", key)
				}
			}
		})
	}
}
