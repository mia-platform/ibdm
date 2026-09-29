// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/config"
	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/mia-platform/ibdm/internal/mappings"
)

// testMappingConfig parses a one-mapping document named name, of type mappingType, writing
// itemFamily under an internal apiVersion, with an optional extra block and syncable line.
func testMappingConfig(tb testing.TB, name, mappingType, itemFamily, extra, syncable string) *config.MappingConfig {
	tb.Helper()

	document := "type: " + mappingType + "\napiVersion: internal.mia-platform.eu/v1\nitemFamily: " + itemFamily + "\n" + extra + syncable +
		"mappings:\n  identifier: \"{{ .id }}\"\n  spec:\n    key: \"{{ .value }}\"\n"
	configs, err := config.NewMappingConfigsFromReader(strings.NewReader(document), "my-source/"+name+".yaml")
	require.NoError(tb, err)
	require.Len(tb, configs, 1)
	return configs[0]
}

// testInternalSet returns four internal mappings: a, b, c, and d, which is not syncable.
func testInternalSet(tb testing.TB) []*config.MappingConfig {
	tb.Helper()

	return []*config.MappingConfig{
		testMappingConfig(tb, "c", "c-type", "c-items", "", ""),
		testMappingConfig(tb, "a", "a-type", "a-items", "", ""),
		testMappingConfig(tb, "d", "d-type", "d-items", "", "syncable: false\n"),
		testMappingConfig(tb, "b", "b-type", "b-items", "", ""),
	}
}

func TestSelectInternalMappings(t *testing.T) {
	t.Parallel()

	include := func(values ...string) internalSelection {
		return internalSelection{include: values, includeSet: true}
	}
	withExclude := func(selection internalSelection, values ...string) internalSelection {
		selection.exclude = values
		selection.excludeSet = true
		return selection
	}

	testCases := map[string]struct {
		selection           internalSelection
		externalNames       []string
		syncOnly            bool
		expectedNames       []string
		expectedNotSyncable []string
		expectedError       error
		expectedMessage     string
		expectedWarning     string
	}{
		"no flag selects nothing, silently": {
			expectedNames: []string{},
		},
		"all selects every internal mapping": {
			selection:     include("all"),
			expectedNames: []string{"a", "b", "c", "d"},
		},
		"a list selects its names in lexical order": {
			selection:     include("c", "a"),
			expectedNames: []string{"a", "c"},
		},
		"all combined with names is an error": {
			selection:     include("all", "a"),
			expectedError: errAllWithOtherMappings,
		},
		"all minus the excluded names": {
			selection:     withExclude(include("all"), "a", "c"),
			expectedNames: []string{"b", "d"},
		},
		"a list minus the excluded names": {
			selection:     withExclude(include("a", "b", "c"), "a"),
			expectedNames: []string{"b", "c"},
		},
		"excluding every included name selects nothing, silently": {
			selection:     withExclude(include("a", "b"), "a", "b"),
			expectedNames: []string{},
		},
		"exclude alone selects nothing and warns": {
			selection:       withExclude(internalSelection{}, "a"),
			expectedNames:   []string{},
			expectedWarning: "without --include-internal-mappings selects no internal mapping",
		},
		"an empty include value selects nothing and warns": {
			selection:       internalSelection{include: []string{}, includeSet: true},
			expectedNames:   []string{},
			expectedWarning: "--include-internal-mappings was given an empty value",
		},
		"a blank include value selects nothing and warns": {
			selection:       include(" "),
			expectedNames:   []string{},
			expectedWarning: "--include-internal-mappings was given an empty value",
		},
		"an empty exclude value is ignored with a warning": {
			selection:       internalSelection{include: []string{"a"}, includeSet: true, exclude: []string{}, excludeSet: true},
			expectedNames:   []string{"a"},
			expectedWarning: "--exclude-internal-mappings was given an empty value",
		},
		"an empty element is an error": {
			selection:       include("a", "", "b"),
			expectedError:   errEmptyMappingName,
			expectedMessage: `"a,,b"`,
		},
		"whitespace is trimmed": {
			selection:     include(" a", "b "),
			expectedNames: []string{"a", "b"},
		},
		"duplicates are counted once": {
			selection:     include("a", "a"),
			expectedNames: []string{"a"},
		},
		"an unknown included name is an error listing the valid names": {
			selection:       include("a", "z"),
			expectedError:   mappings.ErrUnknownMapping,
			expectedMessage: "the internal mappings of my-source are a, b, c, d",
		},
		"an unknown name matching an external mapping says the flags do not apply to it": {
			selection:       include("my-external"),
			externalNames:   []string{"my-external"},
			expectedError:   mappings.ErrUnknownMapping,
			expectedMessage: "the selection flags apply to internal mappings only",
		},
		"an unknown excluded name is an error": {
			selection:     withExclude(include("all"), "z"),
			expectedError: mappings.ErrUnknownMapping,
		},
		"excluding all is an error": {
			selection:     withExclude(include("all"), "all"),
			expectedError: errExcludeAllMappings,
		},
		"excluding a name that was not included warns": {
			selection:       withExclude(include("a", "b"), "c"),
			expectedNames:   []string{"a", "b"},
			expectedWarning: "excluded internal mapping was not selected",
		},
		"sync skips a non syncable mapping reached through all": {
			selection:           include("all"),
			syncOnly:            true,
			expectedNames:       []string{"a", "b", "c"},
			expectedNotSyncable: []string{"d"},
		},
		"sync rejects a non syncable mapping named explicitly": {
			selection:       include("a", "d"),
			syncOnly:        true,
			expectedError:   errNotSyncableMapping,
			expectedMessage: `"d"`,
		},
		"run keeps a non syncable mapping": {
			selection:     include("d"),
			expectedNames: []string{"d"},
		},
		"sync with every selected mapping non syncable selects nothing": {
			selection:           withExclude(include("all"), "a", "b", "c"),
			syncOnly:            true,
			expectedNames:       []string{},
			expectedNotSyncable: []string{"d"},
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			logs := &bytes.Buffer{}
			selected, err := selectInternalMappings(logger.NewLogger(logs), "my-source", test.selection, testInternalSet(t), test.externalNames, test.syncOnly)
			if test.expectedError != nil {
				require.ErrorIs(t, err, test.expectedError)
				require.ErrorContains(t, err, test.expectedMessage)
				return
			}

			require.NoError(t, err)
			require.Equal(t, test.expectedNames, mappingNames(selected.configs))
			require.Equal(t, test.expectedNotSyncable, selected.notSyncable)
			if test.expectedWarning == "" {
				require.NotContains(t, logs.String(), `"@level":"warn"`)
			} else {
				require.Contains(t, logs.String(), test.expectedWarning)
			}
		})
	}
}
