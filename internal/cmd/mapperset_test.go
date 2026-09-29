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

func TestResolveMappersRefusesReservedDomains(t *testing.T) {
	t.Parallel()

	// document returns an external mapping publishing to apiVersion, with an optional relationship
	// extra item, which may always target the reserved relationships item type.
	document := func(apiVersion string, withRelationship bool) string {
		content := "type: a-type\napiVersion: " + apiVersion + "\nitemFamily: my-items\nmappings:\n  identifier: \"{{ .id }}\"\n  spec:\n    key: \"{{ .value }}\"\n"
		if withRelationship {
			content += "  extra:\n    - apiVersion: mia-platform.eu/v1\n      itemFamily: relationships\n      deletePolicy: none\n" +
				"      identifier: \"{{ .id }}-rel\"\n      sourceRef: \"urn:x\"\n      targetRef: \"urn:y\"\n      typeRef: \"urn:z\"\n"
		}
		return content
	}

	testCases := map[string]struct {
		fileName         string
		apiVersion       string
		withRelationship bool
		expectedDomain   string
		expectedFix      string
	}{
		"mia-platform.eu bare":                                 {apiVersion: "mia-platform.eu/v1", expectedDomain: "mia-platform.eu"},
		"mia-platform.eu subdomain":                            {apiVersion: "console.mia-platform.eu/v1", expectedDomain: "mia-platform.eu"},
		"mia-platform.eu other version":                        {apiVersion: "mia-platform.eu/v2", expectedDomain: "mia-platform.eu"},
		"mia-care.io bare":                                     {apiVersion: "mia-care.io/v1", expectedDomain: "mia-care.io"},
		"mia-care.io subdomain":                                {apiVersion: "records.mia-care.io/v1", expectedDomain: "mia-care.io"},
		"mia-care.io other version":                            {apiVersion: "mia-care.io/v2", expectedDomain: "mia-care.io"},
		"mia-fintech.io bare":                                  {apiVersion: "mia-fintech.io/v1", expectedDomain: "mia-fintech.io"},
		"mia-fintech.io subdomain":                             {apiVersion: "payments.mia-fintech.io/v1", expectedDomain: "mia-fintech.io"},
		"mia-fintech.io other version":                         {apiVersion: "mia-fintech.io/v2", expectedDomain: "mia-fintech.io"},
		"a near miss is accepted":                              {apiVersion: "notmia-platform.eu.example.com/v1"},
		"a prefix near miss is accepted":                       {apiVersion: "xmia-care.io/v1"},
		"mia-platform-experimental.eu is free":                 {apiVersion: "console.mia-platform-experimental.eu/v1"},
		"a customer domain is accepted":                        {apiVersion: "my-org.example.com/v1"},
		"a relationship extra on the reserved ITD is accepted": {apiVersion: "my-org.example.com/v1", withRelationship: true},
		"the fix names the internal mapping of the same name": {
			fileName:       "a.yaml",
			apiVersion:     "internal.mia-platform.eu/v1",
			expectedDomain: "mia-platform.eu",
			expectedFix:    "use --include-internal-mappings=a to load the internal mapping instead",
		},
		"without an internal mapping of the same name the fix is another domain": {
			apiVersion:     "internal.mia-platform.eu/v1",
			expectedDomain: "mia-platform.eu",
			expectedFix:    "publish it to your own domain instead",
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fileName := test.fileName
			if fileName == "" {
				fileName = "my-mapping.yaml"
			}
			internal := testInternalSet(t)
			_, err := resolveMappers(logger.NewLogger(&bytes.Buffer{}), mapperRequest{
				source:           "my-source",
				externalPaths:    writeExternalFiles(t, map[string]string{fileName: document(test.apiVersion, test.withRelationship)}),
				internalMappings: func(string) ([]*config.MappingConfig, error) { return internal, nil },
			})
			if test.expectedDomain == "" {
				require.NoError(t, err)
				return
			}

			require.ErrorIs(t, err, errReservedDomain)
			require.ErrorContains(t, err, "reserved domain "+test.expectedDomain)
			require.ErrorContains(t, err, fileName)
			require.ErrorContains(t, err, test.apiVersion)
			require.ErrorContains(t, err, test.expectedFix)
		})
	}

	t.Run("internal mappings on reserved domains are never refused", func(t *testing.T) {
		t.Parallel()

		mappers, err := resolveMappers(logger.NewLogger(&bytes.Buffer{}), mapperRequest{
			source:    "console",
			selection: internalSelection{include: []string{"all"}, includeSet: true},
		})
		require.NoError(t, err)
		require.NotEmpty(t, mappers)
	})
}
