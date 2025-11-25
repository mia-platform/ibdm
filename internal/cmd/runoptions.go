// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package cmd

import (
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/spf13/cobra"
)

const (
	mappingPathFlagName  = "mapping-file"
	mappingPathFlagShort = "f"
	mappingPathFlagUsage = "Path to a file or directory containing custom mapping rules. Can be specified multiple times."
)

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
	}, nil
}

// runOptions holds the options set for the current run function.
type runOptions struct {
	integrationName string
	mappingPaths    []string
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
