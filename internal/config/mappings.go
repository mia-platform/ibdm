// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	DeletePolicyCascade = "cascade"
	DeletePolicyNone    = "none"
)

var (
	// ErrParsing reports failures that occur while decoding mapping files.
	ErrParsing = errors.New("error parsing")
)

// MappingConfig holds the configuration for mapping rules.
type MappingConfig struct {
	Type       string         `json:"type" yaml:"type"`
	Extra      map[string]any `json:"extra,omitempty" yaml:"extra,omitempty"`
	APIVersion string         `json:"apiVersion" yaml:"apiVersion"`
	ItemFamily string         `json:"itemFamily" yaml:"itemFamily"`
	Syncable   bool           `json:"syncable" yaml:"syncable"`
	Mappings   Mappings       `json:"mappings" yaml:"mappings"`
}

// Mappings holds the identifier and specification templates for mapping rules.
type Mappings struct {
	Identifier string            `json:"identifier" yaml:"identifier"`
	Metadata   map[string]string `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	Spec       map[string]string `json:"spec" yaml:"spec"`
	Extra      []Extra           `json:"extra,omitempty" yaml:"extra,omitempty"`
}

type Extra map[string]any

// UnmarshalJSON for special handling of the 'extra' field in mapping configurations.
// It validates the presence and correctness of required fields.
func (e *Extra) UnmarshalJSON(b []byte) error {
	var original map[string]any
	if err := json.Unmarshal(b, &original); err != nil {
		return err
	}

	original, err := validateExtra(original)
	if err != nil {
		return err
	}

	*e = original
	return nil
}

// UnmarshalYAML for special handling of the 'extra' field in mapping configurations.
// It validates the presence and correctness of required fields.
func (e *Extra) UnmarshalYAML(value *yaml.Node) error {
	var original map[string]any
	if err := value.Decode(&original); err != nil {
		return err
	}

	original, err := validateExtra(original)
	if err != nil {
		return err
	}

	*e = original
	return nil
}

func validateExtra(original map[string]any) (map[string]any, error) {
	errorsList := []string{}

	if original["apiVersion"].(string) == "" {
		errorsList = append(errorsList, "unknown field 'apiVersion' in extra mapping")
	}

	if original["itemFamily"].(string) == "" {
		errorsList = append(errorsList, "unknown field 'itemFamily' in extra mapping")
	}

	if original["deletePolicy"].(string) != "" &&
		original["deletePolicy"].(string) != DeletePolicyNone &&
		original["deletePolicy"].(string) != DeletePolicyCascade {
		errorsList = append(errorsList, "unknown value 'deletePolicy' in extra mapping")
	}

	if original["deletePolicy"].(string) != "" {
		original["deletePolicy"] = DeletePolicyNone
	}

	if original["identifier"].(string) == "" {
		errorsList = append(errorsList, "unknown field 'identifier' in extra mapping")
	}

	if original["type"].(string) == "" {
		errorsList = append(errorsList, "unknown field 'type' in extra mapping")
	}

	if len(errorsList) > 0 {
		return nil, fmt.Errorf("invalid extra mapping: %s", strings.Join(errorsList, "; "))
	}

	return original, nil
}

// NewMappingConfigsFromPath parses the file or directory at path and returns any mapping
// configurations it contains. It reports failures encountered while reading or decoding the data.
func NewMappingConfigsFromPath(path string) ([]*MappingConfig, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// Create a YAML decoder for the file.
	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)

	configs := make([]*MappingConfig, 0)

	// Continue parsing until the end of the file.
	for {
		config := new(MappingConfig)
		err := decoder.Decode(&config)
		if err != nil {
			// End of file reached, stop parsing.
			if errors.Is(err, io.EOF) {
				break
			}

			// A different parsing error occurred; return it.
			return nil, fmt.Errorf("%w %q: %w", ErrParsing, path, err)
		}

		// Skip empty configs.
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
		if config.ItemFamily == "" {
			missingFields = append(missingFields, "itemFamily")
		}

		if len(missingFields) > 0 {
			return nil, fmt.Errorf("%w %q: missing required fields: %v", ErrParsing, path, strings.Join(missingFields, ", "))
		}

		configs = append(configs, config)
	}

	return configs, nil
}
