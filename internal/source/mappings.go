// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package source

import (
	"maps"
	"slices"
)

// MappingGroup gathers the mappings of a type that share one value of a grouping key, such as
// the API version they fetch their data with.
type MappingGroup struct {
	// Key is the value the mappings of the group share.
	Key string
	// Mappings lists the names of the mappings in the group, in lexical order.
	Mappings []string
}

// Names returns the names of the mappings in lexical order.
func (m MappingExtras) Names() []string {
	return slices.Sorted(maps.Keys(m))
}

// GroupBy groups the mappings by the key keyOf extracts from their extra. Groups are returned
// in lexical order of key and, within a group, mappings in lexical order of name. A mapping for
// which keyOf reports false belongs to no group.
func (m MappingExtras) GroupBy(keyOf func(extra Extra) (string, bool)) []MappingGroup {
	byKey := make(map[string][]string)
	for _, name := range m.Names() {
		if key, ok := keyOf(m[name]); ok {
			byKey[key] = append(byKey[key], name)
		}
	}

	groups := make([]MappingGroup, 0, len(byKey))
	for _, key := range slices.Sorted(maps.Keys(byKey)) {
		groups = append(groups, MappingGroup{Key: key, Mappings: byKey[key]})
	}

	return groups
}

// Target returns the Data.Mappings value of an emission meant for the mappings named in
// selected. It returns nil when selected covers every mapping in m, so that an emission every
// mapping receives stays untargeted, and the distinct names of selected in lexical order
// otherwise. An empty selected, when m holds any mapping, yields an empty non-nil slice, which
// the pipeline rejects: a source must not emit when it selected no mapping.
func (m MappingExtras) Target(selected []string) []string {
	covered := 0
	for name := range m {
		if slices.Contains(selected, name) {
			covered++
		}
	}
	if covered == len(m) {
		return nil
	}

	target := make([]string, 0, len(selected))
	target = append(target, selected...)
	slices.Sort(target)
	return slices.Compact(target)
}

// InvalidStringValues returns, in lexical order, the names of the mappings whose extra holds
// key with a value that is not a non-empty string. Mappings that do not declare key are not
// reported.
func (m MappingExtras) InvalidStringValues(key string) []string {
	invalid := make([]string, 0)
	for _, name := range m.Names() {
		value, declared := m[name][key]
		if !declared {
			continue
		}

		if text, ok := value.(string); !ok || text == "" {
			invalid = append(invalid, name)
		}
	}

	return invalid
}
