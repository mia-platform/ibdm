// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package cmd

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"sync"

	"github.com/spf13/cobra"

	"github.com/mia-platform/ibdm/internal/config"
	"github.com/mia-platform/ibdm/internal/mapper"
	"github.com/mia-platform/ibdm/internal/pipeline"
)

const (
	mappingPathFlagName  = "mapping-file"
	mappingPathFlagShort = "f"
	mappingPathFlagUsage = "Path to a file or directory containing custom mapping rules. Can be specified multiple times."
)

// sourceGetter is a function that returns a pipeline source based on the provided integration name.
// It can be overridden for testing purposes.
var sourceGetter = sourceFromIntegrationName

// runFlags holds the flags for the "run" command.
type runFlags struct {
	mappingPaths []string
}

// addFlags adds the cli flags to the cobra command.
func (f *runFlags) addFlags(cmd *cobra.Command) {
	cmd.Flags().StringArrayVarP(
		&f.mappingPaths,
		mappingPathFlagName,
		mappingPathFlagShort,
		nil,
		mappingPathFlagUsage)
}

// toOptions converts the run flags to runOptions enriching it with the passed arguments.
func (f *runFlags) toOptions(args []string) (*runOptions, error) {
	integrationName := ""
	if len(args) > 0 {
		integrationName = args[0]
	}

	mappingPaths, err := collectPaths(f.mappingPaths)
	if err != nil {
		return nil, err
	}

	return &runOptions{
		integrationName: integrationName,
		mappingPaths:    mappingPaths,
		destination:     nil, // TODO: create a real destination based on flags
	}, nil
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

// runOptions holds the options set for the current run function.
type runOptions struct {
	integrationName string
	mappingPaths    []string
	destination     pipeline.DataDestination

	lock sync.Mutex
}

// validate validates the run options and returns an error if something is wrong.
func (o *runOptions) validate() error {
	if o.integrationName == "" {
		return errNoArguments
	}

	validIntegrations := map[string]bool{
		"gcp": true,
	}
	if !validIntegrations[o.integrationName] {
		return fmt.Errorf("%w: %s", errInvalidIntegration, o.integrationName)
	}

	return nil
}

// execute starts a data pipeline based on the run options.
func (o *runOptions) execute(ctx context.Context) error {
	if !o.lock.TryLock() {
		return nil
	}
	defer o.lock.Unlock()

	mappers, err := loadMappers(o.mappingPaths, false)
	if err != nil {
		return err
	}

	pipeline := pipeline.New(sourceGetter(o.integrationName), mappers, o.destination)
	return pipeline.Start(ctx)
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
func sourceFromIntegrationName(integrationName string) any {
	return nil
}
