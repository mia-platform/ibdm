// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package cmd

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mia-platform/ibdm/internal/config"
	"github.com/mia-platform/ibdm/internal/mapper"
	"github.com/mia-platform/ibdm/internal/source/gcp"
)

var (
	errNoArguments        = errors.New("no integration name provided")
	errInvalidIntegration = errors.New("invalid integration name provided")

	// availableEventSources holds the list of available integration sources and their description
	// for command completion and help messages.
	availableEventSources = map[string]string{
		"gcp": "Google Cloud Platform integration",
	}
	// availableSyncSources holds the list of available synchronization sources and their description
	// for command completion and help messages.
	availableSyncSources = map[string]string{
		"gcp": "Google Cloud Platform synchronization",
	}

	// sourceGetter is a function that returns a pipeline source based on the provided integration name.
	// It can be overridden for testing purposes.
	sourceGetter = sourceFromIntegrationName
)

// handleError will do custom print error handling based on the type of error received.
// it will return nil if the command must return 0 exit code, otherwise it will return
// the original error.
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

// unwrappedError returns the unwrapped error if available, otherwise it returns the original error.
func unwrappedError(err error) error {
	if unwrapped := errors.Unwrap(err); unwrapped != nil {
		return unwrapped
	}

	return err
}

func validArgsFunc(sources map[string]string) cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
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

func collectPaths(paths []string) ([]string, error) {
	collected := make([]string, 0)
	for _, p := range paths {
		cleanedPath := filepath.Clean(p)
		err := filepath.Walk(cleanedPath, func(walkedPath string, info fs.FileInfo, err error) error {
			if err != nil {
				return fmt.Errorf("mapping file %q: %w", walkedPath, unwrappedError(err))
			}

			switch {
			case !info.IsDir(): // it's a file add to the collection
				collected = append(collected, walkedPath)
			case info.IsDir() && cleanedPath != walkedPath: // skip directories if is not the root path
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

// loadMappers loads the mapping configurations from the provided paths and
// returns a map of typed mappers. If syncOnly is true, only mappings
// of syncable types are loaded.
func loadMappers(paths []string, syncOnly bool) (map[string]mapper.Mapper, error) {
	mappings, err := loadMappingConfigs(paths)
	if err != nil {
		return nil, err
	}

	typedMappers := make(map[string]mapper.Mapper)
	for _, mapping := range mappings {
		if syncOnly && !mapping.Syncable {
			continue
		}

		mappings := mapping.Mappings
		mapper, err := mapper.New(mappings.Identifier, mappings.Spec)
		if err != nil {
			return nil, err
		}

		typedMappers[mapping.Type] = mapper
	}

	return typedMappers, nil
}

// loadMappingConfigs loads all mapping configurations from the provided paths.
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

// sourceFromIntegrationName return a pipeline source based on the provided integrationName.
func sourceFromIntegrationName(ctx context.Context, integrationName string) (any, error) {
	if integrationName == "gcp" {
		return gcp.NewGCPSource(ctx)
	}

	return nil, nil
}
