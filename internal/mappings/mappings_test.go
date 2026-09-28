// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package mappings

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

// testMapping returns a minimal valid mapping document of type mappingType.
func testMapping(mappingType string) []byte {
	return []byte(`type: ` + mappingType + `
apiVersion: my-org.example.com/v1
itemFamily: my-items
mappings:
  identifier: "{{ .id }}"
  spec:
    key: "{{ .value }}"
`)
}

func TestSources(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{"azure", "azure-devops", "bitbucket", "console", "gcp", "github", "gitlab", "nexus", "sysdig"}, Sources())
}

func TestNamesForSourceAndRaw(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"my-source/b-mapping.yaml": {Data: testMapping("b-type")},
		"my-source/a-mapping.yaml": {Data: testMapping("a-type")},
		"my-source/README.md":      {Data: []byte("not a mapping")},
		"broken/bad.yaml":          {Data: []byte("type: [")},
	}
	directories := map[string]string{"my-source": "my-source", "broken": "broken", "empty": "empty"}

	t.Run("names are the yaml base names in lexical order", func(t *testing.T) {
		t.Parallel()
		mappingNames, err := names(fsys, directories, "my-source")
		require.NoError(t, err)
		require.Equal(t, []string{"a-mapping", "b-mapping"}, mappingNames)
	})

	t.Run("mappings are parsed in lexical order of name", func(t *testing.T) {
		t.Parallel()
		configs, err := forSource(fsys, directories, "my-source")
		require.NoError(t, err)
		require.Len(t, configs, 2)
		require.Equal(t, "a-mapping", configs[0].Name)
		require.Equal(t, "a-type", configs[0].Type)
		require.Equal(t, "b-mapping", configs[1].Name)
	})

	t.Run("raw returns the file byte for byte", func(t *testing.T) {
		t.Parallel()
		data, err := raw(fsys, directories, "my-source", "b-mapping")
		require.NoError(t, err)
		require.Equal(t, testMapping("b-type"), data)
	})

	t.Run("unknown source", func(t *testing.T) {
		t.Parallel()
		_, err := names(fsys, directories, "unknown")
		require.ErrorIs(t, err, ErrUnknownSource)
		require.ErrorContains(t, err, "my-source")
		_, err = forSource(fsys, directories, "unknown")
		require.ErrorIs(t, err, ErrUnknownSource)
		_, err = raw(fsys, directories, "unknown", "a-mapping")
		require.ErrorIs(t, err, ErrUnknownSource)
	})

	t.Run("unknown mapping lists the valid names", func(t *testing.T) {
		t.Parallel()
		_, err := raw(fsys, directories, "my-source", "c-mapping")
		require.ErrorIs(t, err, ErrUnknownMapping)
		require.ErrorContains(t, err, "a-mapping, b-mapping")
	})

	t.Run("an unparsable bundled file", func(t *testing.T) {
		t.Parallel()
		_, err := forSource(fsys, directories, "broken")
		require.ErrorIs(t, err, ErrInvalidBundledMapping)
		require.ErrorContains(t, err, "broken/bad.yaml")
	})

	t.Run("a registered source without its directory", func(t *testing.T) {
		t.Parallel()
		_, err := names(fsys, directories, "empty")
		require.ErrorIs(t, err, ErrInvalidBundledMapping)
	})
}

func TestEmbeddedAPI(t *testing.T) {
	t.Parallel()

	mappingNames, err := Names("console")
	require.NoError(t, err)
	require.Contains(t, mappingNames, "projects")

	configs, err := ForSource("console")
	require.NoError(t, err)
	require.Len(t, configs, len(mappingNames))

	data, err := Raw("console", "projects")
	require.NoError(t, err)
	require.Contains(t, string(data), "type: project")
}

func TestReservedDomains(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		apiVersion     string
		expectedDomain string
	}{
		"mia-platform.eu bare":                  {apiVersion: "mia-platform.eu/v1", expectedDomain: "mia-platform.eu"},
		"mia-platform.eu subdomain":             {apiVersion: "console.mia-platform.eu/v1", expectedDomain: "mia-platform.eu"},
		"mia-platform.eu other version":         {apiVersion: "mia-platform.eu/v2", expectedDomain: "mia-platform.eu"},
		"mia-platform.eu near miss":             {apiVersion: "notmia-platform.eu.example.com/v1"},
		"mia-platform.eu prefix near miss":      {apiVersion: "xmia-platform.eu/v1"},
		"mia-platform-experimental.eu is free":  {apiVersion: "console.mia-platform-experimental.eu/v1"},
		"mia-care.io bare":                      {apiVersion: "mia-care.io/v1", expectedDomain: "mia-care.io"},
		"mia-care.io subdomain":                 {apiVersion: "records.mia-care.io/v1", expectedDomain: "mia-care.io"},
		"mia-care.io near miss":                 {apiVersion: "mia-care.io.example.com/v1"},
		"mia-fintech.io bare":                   {apiVersion: "mia-fintech.io/v1", expectedDomain: "mia-fintech.io"},
		"mia-fintech.io subdomain":              {apiVersion: "payments.mia-fintech.io/v1", expectedDomain: "mia-fintech.io"},
		"mia-fintech.io near miss":              {apiVersion: "notmia-fintech.io/v1"},
		"customer domain":                       {apiVersion: "my-org.example.com/v1"},
		"apiVersion without version is a group": {apiVersion: "mia-platform.eu", expectedDomain: "mia-platform.eu"},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			domain, reserved := ReservedAPIVersionDomain(test.apiVersion)
			require.Equal(t, test.expectedDomain, domain)
			require.Equal(t, test.expectedDomain != "", reserved)
		})
	}
}
