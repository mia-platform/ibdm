// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package mapper

// Mapper will define how to map input data to an output structure defined by its Templates.
// Identifier is a special fields used to uniquely identify an entity and is required and will
// always be mapped in the `metadata.name` field of the output.
// All the string values will be used as go string templates to generate its value from the input data.
type Mapper struct {
	Identifier string
	Templates  map[string]string
}

// ApplyTemplates applies the mapper templates to the given input data and returns the mapped output.
func (m *Mapper) ApplyTemplates(_ map[string]any) (map[string]any, error) {
	return nil, nil
}
