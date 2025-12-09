// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	ErrParsing = errors.New("error parsing")
)

// MappingConfig holds the configuration for mapping rules.
type MappingConfig struct {
	Type       string   `json:"type" yaml:"type"`
	APIVersion string   `json:"apiVersion" yaml:"apiVersion"`
	Kind       string   `json:"kind" yaml:"kind"`
	Syncable   bool     `json:"syncable" yaml:"syncable"`
	Mappings   Mappings `json:"mappings" yaml:"mappings"`
}

// Mappings holds the identifier and specification templates for mapping rules.
type Mappings struct {
	Identifier string            `json:"identifier" yaml:"identifier"`
	Spec       map[string]string `json:"spec" yaml:"spec"`
}

// NewMappingConfigsFromPath parse the file or directory at the given path and returns the mapping
// configurations that were found.
// Return an error if something in reading the files or in the parsing process fails.
func NewMappingConfigsFromPath(path string) ([]*MappingConfig, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// create a new yaml decoder for the file
	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)

	configs := make([]*MappingConfig, 0)

	// continue to parse until we reach the end of the file
	for {
		config := new(MappingConfig)
		err := decoder.Decode(&config)
		if err != nil {
			// end of file reached, stop parsing
			if errors.Is(err, io.EOF) {
				break
			}

			// other error occurred during parsing, stop and return it
			return nil, fmt.Errorf("%w %q: %w", ErrParsing, path, err)
		}

		// skip empty configs
		if config == nil {
			continue
		}

		missingFields := []string{}
		if config.Type == "" {
			missingFields = append(missingFields, "type")
		}
		if config.APIVersion == "" {
			missingFields = append(missingFields, "apiVersion")
		}
		if config.Kind == "" {
			missingFields = append(missingFields, "kind")
		}

		if len(missingFields) > 0 {
			return nil, fmt.Errorf("%w %q: missing required fields: %v", ErrParsing, path, strings.Join(missingFields, ", "))
		}

		configs = append(configs, config)
	}

	return configs, nil
}
