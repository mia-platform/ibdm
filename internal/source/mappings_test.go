// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package source

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMappingExtrasNames(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		extras   MappingExtras
		expected []string
	}{
		"names in lexical order": {
			extras:   MappingExtras{"c-mapping": nil, "a-mapping": {}, "b-mapping": {"key": "value"}},
			expected: []string{"a-mapping", "b-mapping", "c-mapping"},
		},
		"no mappings": {
			extras:   nil,
			expected: []string{},
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.ElementsMatch(t, test.expected, test.extras.Names())
			require.IsIncreasing(t, test.extras.Names())
		})
	}
}

func TestMappingExtrasGroupBy(t *testing.T) {
	t.Parallel()

	versionOf := func(extra Extra) (string, bool) {
		version, ok := extra["apiVersion"].(string)
		return version, ok
	}

	testCases := map[string]struct {
		extras   MappingExtras
		expected []MappingGroup
	}{
		"groups and names in lexical order": {
			extras: MappingExtras{
				"d-mapping": {"apiVersion": "2025-01-01"},
				"c-mapping": {"apiVersion": "2024-01-01"},
				"b-mapping": {"apiVersion": "2025-01-01"},
				"a-mapping": {"apiVersion": "2024-01-01"},
			},
			expected: []MappingGroup{
				{Key: "2024-01-01", Mappings: []string{"a-mapping", "c-mapping"}},
				{Key: "2025-01-01", Mappings: []string{"b-mapping", "d-mapping"}},
			},
		},
		"mappings without a key belong to no group": {
			extras: MappingExtras{
				"a-mapping": {"apiVersion": "2024-01-01"},
				"b-mapping": nil,
				"c-mapping": {"apiVersion": 123},
			},
			expected: []MappingGroup{
				{Key: "2024-01-01", Mappings: []string{"a-mapping"}},
			},
		},
		"no mappings": {
			extras:   nil,
			expected: []MappingGroup{},
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.expected, test.extras.GroupBy(versionOf))
		})
	}
}

func TestMappingExtrasTarget(t *testing.T) {
	t.Parallel()

	extras := MappingExtras{"a-mapping": nil, "b-mapping": nil, "c-mapping": nil}

	testCases := map[string]struct {
		extras   MappingExtras
		selected []string
		expected []string
	}{
		"every mapping selected stays untargeted": {
			extras:   extras,
			selected: []string{"c-mapping", "a-mapping", "b-mapping"},
			expected: nil,
		},
		"every mapping selected with an unknown one stays untargeted": {
			extras:   extras,
			selected: []string{"a-mapping", "b-mapping", "c-mapping", "unknown"},
			expected: nil,
		},
		"a subset is targeted in lexical order": {
			extras:   extras,
			selected: []string{"c-mapping", "a-mapping"},
			expected: []string{"a-mapping", "c-mapping"},
		},
		"duplicate names are collapsed": {
			extras:   extras,
			selected: []string{"b-mapping", "b-mapping"},
			expected: []string{"b-mapping"},
		},
		"nothing selected is an empty non nil target": {
			extras:   extras,
			selected: nil,
			expected: []string{},
		},
		"no mappings registered stays untargeted": {
			extras:   MappingExtras{},
			selected: nil,
			expected: nil,
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			target := test.extras.Target(test.selected)
			require.Equal(t, test.expected, target)
			require.Equal(t, test.expected == nil, target == nil)
		})
	}

	t.Run("selected is not modified", func(t *testing.T) {
		t.Parallel()

		selected := []string{"c-mapping", "a-mapping"}
		extras.Target(selected)
		require.Equal(t, []string{"c-mapping", "a-mapping"}, selected)
	})
}

func TestMappingExtrasInvalidStringValues(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		extras   MappingExtras
		expected []string
	}{
		"non string and empty values are reported in lexical order": {
			extras: MappingExtras{
				"e-mapping": {"apiVersion": 123},
				"d-mapping": {"apiVersion": "2024-01-01"},
				"c-mapping": {"apiVersion": ""},
				"b-mapping": nil,
				"a-mapping": {"apiVersion": nil},
			},
			expected: []string{"a-mapping", "c-mapping", "e-mapping"},
		},
		"valid values report nothing": {
			extras:   MappingExtras{"a-mapping": {"apiVersion": "2024-01-01"}},
			expected: []string{},
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.expected, test.extras.InvalidStringValues("apiVersion"))
		})
	}
}
