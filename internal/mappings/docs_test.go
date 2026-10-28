// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package mappings

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// referencePage lists every internal mapping for the users.
var referencePage = filepath.Join("..", "..", "docs", "reference", "20_internal-mappings.md")

// mappingRowRegex captures the name of every mapping row of the reference page.
var mappingRowRegex = regexp.MustCompile("(?m)^\\| `([^`]+)` \\|")

// TestReferencePageListsEveryInternalMapping keeps the reference page in step with the embedded
// mappings: each integration section lists exactly its internal mappings, in lexical order.
func TestReferencePageListsEveryInternalMapping(t *testing.T) {
	t.Parallel()

	content, err := os.ReadFile(referencePage)
	require.NoError(t, err)

	sections := strings.Split(string(content), "\n## `")
	listed := make(map[string][]string, len(sections))
	for _, section := range sections[1:] {
		source, body, found := strings.Cut(section, "`")
		require.True(t, found)
		for _, match := range mappingRowRegex.FindAllStringSubmatch(body, -1) {
			listed[source] = append(listed[source], match[1])
		}
	}

	require.Len(t, listed, len(Sources()), "every integration needs its section in %s", referencePage)
	for _, source := range Sources() {
		names, err := Names(source)
		require.NoError(t, err)
		require.Equal(t, names, listed[source], "the %q section of %s must list exactly its internal mappings", source, referencePage)
	}
}
