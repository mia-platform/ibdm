// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/config"
	"github.com/mia-platform/ibdm/internal/logger"
)

// externalDocument is an external mapping document of type mappingType writing itemFamily in a
// customer domain, with an optional extra block and syncable line.
func externalDocument(mappingType, itemFamily, extra, syncable string) string {
	return "type: " + mappingType + "\napiVersion: my-org.example.com/v1\nitemFamily: " + itemFamily + "\n" + extra + syncable +
		"mappings:\n  identifier: \"{{ .id }}\"\n  spec:\n    key: \"{{ .value }}\"\n"
}

// writeExternalFiles writes files into a temporary directory and returns their paths in lexical order.
func writeExternalFiles(tb testing.TB, files map[string]string) []string {
	tb.Helper()

	dir := tb.TempDir()
	for name, content := range files {
		require.NoError(tb, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
	}
	paths, err := collectPaths([]string{dir})
	require.NoError(tb, err)
	return paths
}

func TestResolveMappers(t *testing.T) {
	t.Parallel()

	all := internalSelection{include: []string{"all"}, includeSet: true}
	only := func(names ...string) internalSelection { return internalSelection{include: names, includeSet: true} }
	extraVersion := func(version string) string { return "extra:\n  apiVersion: \"" + version + "\"\n" }

	testCases := map[string]struct {
		internal             func(tb testing.TB) []*config.MappingConfig
		selection            internalSelection
		external             map[string]string
		syncOnly             bool
		allowSharedItemTypes bool
		expectedOrder        map[string][]string
		expectedError        error
		expectedMessage      string
		expectedLog          []string
		unexpectedLog        []string
	}{
		"internal mappings come before external ones on a shared type": {
			internal:  testInternalSet,
			selection: only("a"),
			external:  map[string]string{"my-a.yaml": externalDocument("a-type", "my-items", "", "")},
			expectedOrder: map[string][]string{
				"a-type": {"a", "my-a"},
			},
			expectedLog: []string{"my-source: using 1 internal mappings and 1 external mappings"},
		},
		"external mappings alone run without internal ones": {
			internal:      testInternalSet,
			external:      map[string]string{"my-a.yaml": externalDocument("a-type", "my-items", "", "")},
			expectedOrder: map[string][]string{"a-type": {"my-a"}},
		},
		"nothing selected returns no mapper and says so": {
			internal:      testInternalSet,
			expectedOrder: map[string][]string{},
			expectedLog:   []string{"my-source: no mappings selected, nothing to do"},
		},
		"an external mapping named all is refused": {
			internal:      testInternalSet,
			external:      map[string]string{"all.yaml": externalDocument("a-type", "my-items", "", "")},
			expectedError: errReservedMappingName,
		},
		"an external mapping named like a selected internal one is refused with the fixes": {
			internal:        testInternalSet,
			selection:       all,
			external:        map[string]string{"b.yaml": externalDocument("b-type", "my-items", "", "")},
			expectedError:   config.ErrDuplicateMappingName,
			expectedMessage: "leave the internal mapping out of the selection",
		},
		"an external mapping named like an internal one left out of the selection loads": {
			internal:      testInternalSet,
			selection:     only("a"),
			external:      map[string]string{"b.yaml": externalDocument("b-type", "my-items", "", "")},
			expectedOrder: map[string][]string{"a-type": {"a"}, "b-type": {"b"}},
		},
		"mappings sharing an item type block the start": {
			internal: testInternalSet,
			external: map[string]string{
				"my-a.yaml": externalDocument("a-type", "shared-items", "", ""),
				"my-b.yaml": externalDocument("b-type", "shared-items", "", ""),
			},
			expectedError:   errSharedItemTypes,
			expectedMessage: "my-org.example.com/v1 shared-items (my-a, my-b): pass --allow-shared-item-types to start anyway",
		},
		"mappings sharing an item type start with the flag and a warning": {
			internal: testInternalSet,
			external: map[string]string{
				"my-a.yaml": externalDocument("a-type", "shared-items", "", ""),
				"my-b.yaml": externalDocument("b-type", "shared-items", "", ""),
			},
			allowSharedItemTypes: true,
			expectedOrder:        map[string][]string{"a-type": {"my-a"}, "b-type": {"my-b"}},
			expectedLog:          []string{"their items may overwrite each other"},
		},
		"one type writing two item types is not flagged": {
			internal: testInternalSet,
			external: map[string]string{
				"my-first.yaml":  externalDocument("a-type", "first-items", "", ""),
				"my-second.yaml": externalDocument("a-type", "second-items", "", ""),
			},
			expectedOrder: map[string][]string{"a-type": {"my-first", "my-second"}},
			unexpectedLog: []string{"overwrite"},
		},
		"an external mapping with a different extra on an internal type warns": {
			internal: func(tb testing.TB) []*config.MappingConfig {
				tb.Helper()
				return []*config.MappingConfig{testMappingConfig(tb, "a", "a-type", "a-items", extraVersion("2026-03-10"), "")}
			},
			selection:     all,
			external:      map[string]string{"my-a.yaml": externalDocument("a-type", "my-items", extraVersion("2022-11-28"), "")},
			expectedOrder: map[string][]string{"a-type": {"a", "my-a"}},
			expectedLog:   []string{"declares a root extra different from the internal mapping of its type", `"externalMapping":"my-a"`},
		},
		"an external mapping with the same or no extra does not warn": {
			internal: func(tb testing.TB) []*config.MappingConfig {
				tb.Helper()
				return []*config.MappingConfig{testMappingConfig(tb, "a", "a-type", "a-items", extraVersion("2026-03-10"), "")}
			},
			selection: all,
			external: map[string]string{
				"my-same.yaml": externalDocument("a-type", "same-items", extraVersion("2026-03-10"), ""),
				"my-none.yaml": externalDocument("a-type", "none-items", "", ""),
			},
			expectedOrder: map[string][]string{"a-type": {"a", "my-none", "my-same"}},
			unexpectedLog: []string{"root extra"},
		},
		"sync skips non syncable mappings and reports them": {
			internal:      testInternalSet,
			selection:     all,
			external:      map[string]string{"my-event.yaml": externalDocument("e-type", "e-items", "", "syncable: false\n")},
			syncOnly:      true,
			expectedOrder: map[string][]string{"a-type": {"a"}, "b-type": {"b"}, "c-type": {"c"}},
			expectedLog:   []string{`"skippedNotSyncable":["d","my-event"]`},
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			logs := &bytes.Buffer{}
			internal := test.internal(t)
			mappers, err := resolveMappers(logger.NewLogger(logs), mapperRequest{
				source:               "my-source",
				selection:            test.selection,
				externalPaths:        writeExternalFiles(t, test.external),
				syncOnly:             test.syncOnly,
				allowSharedItemTypes: test.allowSharedItemTypes,
				internalMappings:     func(string) ([]*config.MappingConfig, error) { return internal, nil },
			})
			if test.expectedError != nil {
				require.ErrorIs(t, err, test.expectedError)
				require.ErrorContains(t, err, test.expectedMessage)
				return
			}

			require.NoError(t, err)
			order := make(map[string][]string, len(mappers))
			for mappingType, typeMappers := range mappers {
				for _, dataMapper := range typeMappers {
					order[mappingType] = append(order[mappingType], dataMapper.Name)
				}
			}
			require.Equal(t, test.expectedOrder, order)
			for _, expected := range test.expectedLog {
				require.Contains(t, logs.String(), expected)
			}
			for _, unexpected := range test.unexpectedLog {
				require.NotContains(t, strings.ToLower(logs.String()), unexpected)
			}
		})
	}
}

func TestResolveMappersInternalError(t *testing.T) {
	t.Parallel()

	_, err := resolveMappers(logger.NewLogger(&bytes.Buffer{}), mapperRequest{
		source:           "my-source",
		internalMappings: func(string) ([]*config.MappingConfig, error) { return nil, os.ErrNotExist },
	})
	require.ErrorIs(t, err, os.ErrNotExist)
}
