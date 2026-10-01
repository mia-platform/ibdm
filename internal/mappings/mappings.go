// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package mappings

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/mia-platform/ibdm/internal/config"
)

// fileExtension is the extension of every bundled mapping file.
const fileExtension = ".yaml"

var (
	// ErrUnknownSource reports a source ibdm bundles no mappings for.
	ErrUnknownSource = errors.New("unknown source")
	// ErrUnknownMapping reports a mapping name that is not bundled for a source.
	ErrUnknownMapping = errors.New("unknown internal mapping")
	// ErrInvalidBundledMapping reports a bundled mapping file that cannot be read or parsed.
	ErrInvalidBundledMapping = errors.New("invalid bundled mapping")

	//go:embed data
	embedded embed.FS

	// bundled is the embedded data directory, so paths read "console/projects.yaml".
	bundled = mustSub(embedded, "data")

	// sourceDirectories binds every source slug to the directory holding its mappings. The binding
	// is explicit on purpose: a new source that forgets its mappings fails the conformance tests
	// instead of silently shipping without them.
	sourceDirectories = map[string]string{
		"azure":        "azure",
		"azure-devops": "azure-devops",
		"bitbucket":    "bitbucket",
		"console":      "console",
		"gcp":          "gcp",
		"github":       "github",
		"gitlab":       "gitlab",
		"nexus":        "nexus",
		"sonarqube":    "sonarqube",
		"sysdig":       "sysdig",
	}
)

// mustSub returns the sub tree dir of fsys. It panics only if dir is not a valid path, which is
// a programming error caught by any test importing the package.
func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err)
	}
	return sub
}

// Sources returns, in lexical order, the sources ibdm bundles mappings for.
func Sources() []string {
	return slices.Sorted(maps.Keys(sourceDirectories))
}

// Names returns, in lexical order, the names of the mappings bundled for source. The name of a
// bundled mapping is the base name of its file.
func Names(source string) ([]string, error) {
	return names(bundled, sourceDirectories, source)
}

// ForSource parses and returns the mappings bundled for source, in lexical order of name.
func ForSource(source string) ([]*config.MappingConfig, error) {
	return forSource(bundled, sourceDirectories, source)
}

// Raw returns the bundled file of the mapping name for source, byte for byte.
func Raw(source, name string) ([]byte, error) {
	return raw(bundled, sourceDirectories, source, name)
}

// directory returns the directory bound to source in directories.
func directory(directories map[string]string, source string) (string, error) {
	dir, ok := directories[source]
	if !ok {
		return "", fmt.Errorf("%w %q: internal mappings exist for %s", ErrUnknownSource, source, strings.Join(slices.Sorted(maps.Keys(directories)), ", "))
	}
	return dir, nil
}

// names lists the mapping names of source in fsys.
func names(fsys fs.FS, directories map[string]string, source string) ([]string, error) {
	dir, err := directory(directories, source)
	if err != nil {
		return nil, err
	}

	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidBundledMapping, err)
	}

	mappingNames := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || path.Ext(entry.Name()) != fileExtension {
			continue
		}
		mappingNames = append(mappingNames, strings.TrimSuffix(entry.Name(), fileExtension))
	}
	return mappingNames, nil
}

// forSource parses every mapping of source in fsys.
func forSource(fsys fs.FS, directories map[string]string, source string) ([]*config.MappingConfig, error) {
	mappingNames, err := names(fsys, directories, source)
	if err != nil {
		return nil, err
	}

	dir := directories[source]
	configs := make([]*config.MappingConfig, 0, len(mappingNames))
	for _, name := range mappingNames {
		fileConfigs, err := config.NewMappingConfigsFromFS(fsys, path.Join(dir, name+fileExtension))
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidBundledMapping, err)
		}
		configs = append(configs, fileConfigs...)
	}
	return configs, nil
}

// raw reads the file of the mapping name of source in fsys.
func raw(fsys fs.FS, directories map[string]string, source, name string) ([]byte, error) {
	mappingNames, err := names(fsys, directories, source)
	if err != nil {
		return nil, err
	}

	if !slices.Contains(mappingNames, name) {
		return nil, fmt.Errorf("%w %q for %s: internal mappings are %s", ErrUnknownMapping, name, source, strings.Join(mappingNames, ", "))
	}

	data, err := fs.ReadFile(fsys, path.Join(directories[source], name+fileExtension))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidBundledMapping, err)
	}
	return data, nil
}
