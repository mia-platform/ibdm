// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package mappings

import (
	"fmt"
	"io/fs"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/mia-platform/ibdm/internal/config"
	"github.com/mia-platform/ibdm/internal/mapper"
)

// cliSources copies the integration names registered by internal/cmd (availableSyncSources and
// availableEventSources). internal/cmd does not export them, so the list is duplicated here and
// the conformance test keeps the two in step.
var cliSources = []string{"azure", "azure-devops", "bitbucket", "console", "gcp", "github", "gitlab", "nexus", "sysdig"}

// reservedMappingName can never be the name of a mapping: it is the selector of every mapping.
const reservedMappingName = "all"

// urnGroupRegex captures the group of every Catalog URN in a mapping file.
var urnGroupRegex = regexp.MustCompile(`urn:mia-platform-catalog:([^:\s"']+):`)

// structuralProblems is conformance tier 1: every bundled file parses through the runtime loader
// into exactly one mapping, named after its file and publishing to a reserved domain, with types
// unique per source, and the registry covers every CLI source and every data directory.
func structuralProblems(fsys fs.FS, directories map[string]string, sources []string) []string {
	problems := make([]string, 0)
	registered := slices.Sorted(maps.Keys(directories))
	for _, source := range sources {
		if !slices.Contains(registered, source) {
			problems = append(problems, fmt.Sprintf("CLI source %q has no bundled mappings", source))
		}
	}
	for _, source := range registered {
		if !slices.Contains(sources, source) {
			problems = append(problems, fmt.Sprintf("registered source %q is not a CLI source", source))
		}
	}

	topLevel, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return append(problems, err.Error())
	}
	for _, entry := range topLevel {
		if !slices.Contains(slices.Collect(maps.Values(directories)), entry.Name()) {
			problems = append(problems, fmt.Sprintf("data entry %q is bound to no source", entry.Name()))
		}
	}

	for _, source := range registered {
		dir := directories[source]
		entries, err := fs.ReadDir(fsys, dir)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", source, err))
			continue
		}
		if len(entries) == 0 {
			problems = append(problems, source+": no bundled mapping")
		}

		types := make(map[string]string)
		for _, entry := range entries {
			file := path.Join(dir, entry.Name())
			if entry.IsDir() || path.Ext(entry.Name()) != fileExtension {
				problems = append(problems, fmt.Sprintf("%s: not a %s mapping file", file, fileExtension))
				continue
			}
			baseName := strings.TrimSuffix(entry.Name(), fileExtension)
			if baseName == reservedMappingName {
				problems = append(problems, fmt.Sprintf("%s: %q is a reserved mapping name", file, reservedMappingName))
			}
			problems = append(problems, fileProblems(fsys, file, baseName, types)...)
		}
	}
	return problems
}

// fileProblems checks one bundled file for structuralProblems, recording its type in types.
func fileProblems(fsys fs.FS, file, baseName string, types map[string]string) []string {
	problems := make([]string, 0)
	data, err := fs.ReadFile(fsys, file)
	if err != nil {
		return append(problems, fmt.Sprintf("%s: %v", file, err))
	}
	var root map[string]any
	if err := yaml.Unmarshal(data, &root); err == nil {
		if _, declared := root[config.NameField]; declared {
			problems = append(problems, fmt.Sprintf("%s: a bundled mapping must not declare %q, its name is its file name", file, config.NameField))
		}
	}

	configs, err := config.NewMappingConfigsFromFS(fsys, file)
	if err != nil {
		return append(problems, fmt.Sprintf("%s: %v", file, err))
	}
	if len(configs) != 1 {
		return append(problems, fmt.Sprintf("%s: holds %d mappings, a bundled file must hold exactly one", file, len(configs)))
	}

	mapping := configs[0]
	if mapping.Name != baseName {
		problems = append(problems, fmt.Sprintf("%s: name %q differs from the file name", file, mapping.Name))
	}
	if mapping.Type == "" || mapping.APIVersion == "" || mapping.ItemFamily == "" || mapping.Mappings.Identifier == "" {
		problems = append(problems, file+": type, apiVersion, itemFamily and mappings.identifier are required")
	}
	if _, reserved := ReservedAPIVersionDomain(mapping.APIVersion); !reserved {
		problems = append(problems, fmt.Sprintf("%s: root apiVersion %q is not under a reserved domain", file, mapping.APIVersion))
	}
	if other, duplicated := types[mapping.Type]; duplicated {
		problems = append(problems, fmt.Sprintf("%s: type %q is already mapped by %s", file, mapping.Type, other))
	}
	types[mapping.Type] = file
	return problems
}

// urnProblems is conformance tier 1b: a bundled mapping only relates system items, so every
// Catalog URN it contains must carry a reserved group.
func urnProblems(fsys fs.FS) []string {
	problems := make([]string, 0)
	err := fs.WalkDir(fsys, ".", func(file string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := fs.ReadFile(fsys, file)
		if err != nil {
			return err
		}
		for _, match := range urnGroupRegex.FindAllStringSubmatch(string(data), -1) {
			if _, reserved := ReservedDomain(match[1]); !reserved {
				problems = append(problems, fmt.Sprintf("%s: URN group %q is not a reserved domain", file, match[1]))
			}
		}
		return nil
	})
	if err != nil {
		problems = append(problems, err.Error())
	}
	return problems
}

// compilationProblems is conformance tier 2: every bundled mapping compiles through the same
// constructor the CLI uses.
func compilationProblems(fsys fs.FS, directories map[string]string) []string {
	problems := make([]string, 0)
	for _, source := range slices.Sorted(maps.Keys(directories)) {
		configs, err := forSource(fsys, directories, source)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", source, err))
			continue
		}
		for _, mapping := range configs {
			templates := mapping.Mappings
			if _, err := mapper.New(templates.Identifier, templates.Metadata, templates.Spec, templates.Extra, mapper.WithCreateIf(mapping.CreateIf)); err != nil {
				problems = append(problems, fmt.Sprintf("%s/%s: %v", source, mapping.Name, err))
			}
		}
	}
	return problems
}

func TestBundledMappingsConformance(t *testing.T) {
	t.Parallel()

	t.Run("tier 1 structural", func(t *testing.T) {
		t.Parallel()
		require.Empty(t, structuralProblems(bundled, sourceDirectories, cliSources))
	})

	t.Run("tier 1b URN domains", func(t *testing.T) {
		t.Parallel()
		require.Empty(t, urnProblems(bundled))
	})

	t.Run("tier 2 compilation", func(t *testing.T) {
		t.Parallel()
		require.Empty(t, compilationProblems(bundled, sourceDirectories))
	})

	t.Run("inventory", func(t *testing.T) {
		t.Parallel()
		total := 0
		for _, source := range Sources() {
			configs, err := ForSource(source)
			require.NoError(t, err)
			total += len(configs)
		}
		require.Equal(t, 43, total, "update this count and the documentation when bundled mappings are added or removed")
	})
}

// validDocument is a bundled-style mapping document of type mappingType.
func validDocument(mappingType string) string {
	return `type: ` + mappingType + `
apiVersion: console.mia-platform.eu/v1
itemFamily: my-items
mappings:
  identifier: "{{ .id }}"
  spec:
    key: "{{ .value }}"
`
}

func TestConformanceChecksCatchDefects(t *testing.T) {
	t.Parallel()

	directories := map[string]string{"my-source": "my-source"}
	testCases := map[string]struct {
		files    map[string]string
		check    func(fsys fs.FS) []string
		expected string
	}{
		"a file declaring its name": {
			files:    map[string]string{"my-source/a.yaml": "name: a\n" + validDocument("a-type")},
			check:    func(fsys fs.FS) []string { return structuralProblems(fsys, directories, []string{"my-source"}) },
			expected: "must not declare",
		},
		"a file holding two mappings": {
			files:    map[string]string{"my-source/a.yaml": "name: a\n" + validDocument("a-type") + "---\nname: b\n" + validDocument("b-type")},
			check:    func(fsys fs.FS) []string { return structuralProblems(fsys, directories, []string{"my-source"}) },
			expected: "exactly one",
		},
		"a file named all": {
			files:    map[string]string{"my-source/all.yaml": validDocument("a-type")},
			check:    func(fsys fs.FS) []string { return structuralProblems(fsys, directories, []string{"my-source"}) },
			expected: "reserved mapping name",
		},
		"a root apiVersion outside the reserved domains": {
			files:    map[string]string{"my-source/a.yaml": strings.Replace(validDocument("a-type"), "console.mia-platform.eu", "my-org.example.com", 1)},
			check:    func(fsys fs.FS) []string { return structuralProblems(fsys, directories, []string{"my-source"}) },
			expected: "not under a reserved domain",
		},
		"two files mapping one type": {
			files:    map[string]string{"my-source/a.yaml": validDocument("same-type"), "my-source/b.yaml": validDocument("same-type")},
			check:    func(fsys fs.FS) []string { return structuralProblems(fsys, directories, []string{"my-source"}) },
			expected: "already mapped",
		},
		"an unknown field": {
			files:    map[string]string{"my-source/a.yaml": "unknownField: true\n" + validDocument("a-type")},
			check:    func(fsys fs.FS) []string { return structuralProblems(fsys, directories, []string{"my-source"}) },
			expected: "unknownField",
		},
		"a file that is not yaml": {
			files:    map[string]string{"my-source/a.yaml": validDocument("a-type"), "my-source/notes.txt": "notes"},
			check:    func(fsys fs.FS) []string { return structuralProblems(fsys, directories, []string{"my-source"}) },
			expected: "not a .yaml mapping file",
		},
		"a CLI source without bundled mappings": {
			files: map[string]string{"my-source/a.yaml": validDocument("a-type")},
			check: func(fsys fs.FS) []string {
				return structuralProblems(fsys, directories, []string{"my-source", "other"})
			},
			expected: `CLI source "other" has no bundled mappings`,
		},
		"a registered source that is not a CLI source": {
			files:    map[string]string{"my-source/a.yaml": validDocument("a-type")},
			check:    func(fsys fs.FS) []string { return structuralProblems(fsys, directories, []string{}) },
			expected: "is not a CLI source",
		},
		"a data directory bound to no source": {
			files:    map[string]string{"my-source/a.yaml": validDocument("a-type"), "orphan/b.yaml": validDocument("b-type")},
			check:    func(fsys fs.FS) []string { return structuralProblems(fsys, directories, []string{"my-source"}) },
			expected: `"orphan" is bound to no source`,
		},
		"a URN outside the reserved domains": {
			files:    map[string]string{"my-source/a.yaml": validDocument("a-type") + "# urn:mia-platform-catalog:console.mia-platform-experimental.eu:v1:Project:null:x\n"},
			check:    urnProblems,
			expected: "console.mia-platform-experimental.eu",
		},
		"a template that does not compile": {
			files:    map[string]string{"my-source/a.yaml": strings.Replace(validDocument("a-type"), "{{ .value }}", "{{ .value | unknownFunc }}", 1)},
			check:    func(fsys fs.FS) []string { return compilationProblems(fsys, directories) },
			expected: "unknownFunc",
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fsys := fstest.MapFS{}
			for file, content := range test.files {
				fsys[file] = &fstest.MapFile{Data: []byte(content)}
			}
			problems := test.check(fsys)
			require.NotEmpty(t, problems)
			require.Contains(t, strings.Join(problems, "\n"), test.expected)
		})
	}
}
