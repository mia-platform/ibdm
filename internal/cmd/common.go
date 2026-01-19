// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mia-platform/ibdm/internal/config"
	"github.com/mia-platform/ibdm/internal/mapper"
	"github.com/mia-platform/ibdm/internal/pipeline"
	"github.com/mia-platform/ibdm/internal/source/azure"
	"github.com/mia-platform/ibdm/internal/source/console"
	"github.com/mia-platform/ibdm/internal/source/gcp"
)

var (
	errNoArguments        = errors.New("no integration name provided")
	errInvalidIntegration = errors.New("invalid integration name provided")

	// availableEventSources covers event-stream integration sources used for completion and help text.
	availableEventSources = map[string]string{
		"azure":   "Microsoft Azure integration",
		"gcp":     "Google Cloud Platform integration",
		"console": "Mia Platform Console integration",
	}
	// availableSyncSources covers synchronization sources used for completion and help text.
	availableSyncSources = map[string]string{
		"azure": "Microsoft Azure integration",
		"gcp":   "Google Cloud Platform synchronization",
	}
)

// handleError formats known errors for the CLI and decides whether to propagate them.
// It returns nil when the command should exit successfully; otherwise it returns the
// original error.
func handleError(cmd *cobra.Command, err error) error {
	switch {
	case errors.Is(err, errNoArguments):
		_ = cmd.Usage() // do not check error as we cannot do much about it
		return nil
	case errors.Is(err, errInvalidIntegration):
		cmd.PrintErrln(err)
		_ = cmd.Usage() // do not check error as we cannot do much about it
		return err
	default:
		cmd.PrintErrln(err)
		return err
	}
}

// validArgsFunc produces a completion function backed by the provided source map.
func validArgsFunc(sources map[string]string) cobra.CompletionFunc {
	return func(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		var comps []string
		if len(args) == 0 {
			for name, description := range sources {
				if strings.HasPrefix(name, toComplete) {
					comps = append(comps, cobra.CompletionWithDesc(name, description))
				}
			}
		}

		return comps, cobra.ShellCompDirectiveNoFileComp
	}
}

// sourceFromIntegrationName returns the pipeline source matching integrationName.
func sourceFromIntegrationName(integrationName string) (any, error) {
	switch integrationName {
	case "azure":
		return azure.NewSource()
	case "gcp":
		return gcp.NewSource()
	}
	if integrationName == "console" {
		return console.NewSource()
	}
	return nil, nil
}

// unwrappedError unwraps err once and falls back to the original error when needed.
func unwrappedError(err error) error {
	if unwrapped := errors.Unwrap(err); unwrapped != nil {
		return unwrapped
	}

	return err
}

// collectPaths discovers every file under the provided paths.
// Directories are walked one level deep to gather their files.
func collectPaths(paths []string) ([]string, error) {
	collected := make([]string, 0)
	for _, p := range paths {
		cleanedPath := filepath.Clean(p)
		err := filepath.Walk(cleanedPath, func(walkedPath string, info fs.FileInfo, err error) error {
			if err != nil {
				return fmt.Errorf("mapping file %q: %w", walkedPath, unwrappedError(err))
			}

			switch {
			case !info.IsDir(): // file found, add it to the collection
				collected = append(collected, walkedPath)
			case info.IsDir() && cleanedPath != walkedPath: // skip nested directories beyond the root path
				return filepath.SkipDir
			}

			return nil
		})

		if err != nil {
			return nil, err
		}
	}

	return collected, nil
}

// loadMappers loads mapping files and builds typed mappers. When syncOnly is true,
// it skips definitions that are not marked as syncable.
func loadMappers(paths []string, syncOnly bool) (map[string]pipeline.DataMapper, error) {
	mappings, err := loadMappingConfigs(paths)
	if err != nil {
		return nil, err
	}

	typedMappers := make(map[string]pipeline.DataMapper)
	for _, mapping := range mappings {
		if syncOnly && !mapping.Syncable {
			continue
		}

		mappings := mapping.Mappings
		mapper, err := mapper.New(mappings.Identifier, mappings.Spec)
		if err != nil {
			return nil, err
		}

		typedMappers[mapping.Type] = pipeline.DataMapper{
			APIVersion: mapping.APIVersion,
			Resource:   mapping.Resource,
			Mapper:     mapper,
			Extra:      mapping.Extra,
		}
	}

	return typedMappers, nil
}

// loadMappingConfigs reads every mapping configuration from the provided paths.
func loadMappingConfigs(paths []string) ([]*config.MappingConfig, error) {
	mappings := make([]*config.MappingConfig, 0)
	for _, path := range paths {
		fileMappings, err := config.NewMappingConfigsFromPath(path)
		if err != nil {
			return nil, err
		}

		mappings = append(mappings, fileMappings...)
	}

	return mappings, nil
}
