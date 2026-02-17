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

	ExtraRelationshipFamily = "relationships"
)

var (
	// ErrParsing reports failures that occur while decoding mapping files.
	ErrParsing = errors.New("error parsing")

	RequiredExtraFields = []string{"apiVersion", "itemFamily", "deletePolicy", "identifier"}
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
	Metadata   MetadataMapping   `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	Spec       map[string]string `json:"spec" yaml:"spec"`
	Extra      []Extra           `json:"extra,omitempty" yaml:"extra,omitempty"`
}

type MetadataMapping map[string]string

type MetadataTemplate struct {
	Annotations       string `json:"annotations,omitempty" yaml:"annotations,omitempty"`
	CreationTimestamp string `json:"creationTimestamp,omitempty" yaml:"creationTimestamp,omitempty"`
	Description       string `json:"description,omitempty" yaml:"description,omitempty"`
	Labels            string `json:"labels,omitempty" yaml:"labels,omitempty"`
	Links             string `json:"links,omitempty" yaml:"links,omitempty"`
	Name              string `json:"name,omitempty" yaml:"name,omitempty"`
	Namespace         string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Tags              string `json:"tags,omitempty" yaml:"tags,omitempty"`
	Title             string `json:"title,omitempty" yaml:"title,omitempty"`
	UID               string `json:"uid,omitempty" yaml:"uid,omitempty"`
}

// UnmarshalYAML for special handling of the 'extra' field in mapping configurations.
// It validates the presence and correctness of required fields.
func (mm *MetadataMapping) UnmarshalYAML(value *yaml.Node) error {
	var original MetadataTemplate
	if err := value.Decode(&original); err != nil {
		return err
	}

	raw, _ := json.Marshal(original)

	var mappings MetadataMapping
	_ = json.Unmarshal(raw, &mappings)
	*mm = mappings

	return nil
}

type Extra map[string]any

func validateExtra(extraMap map[string]any) (map[string]any, error) {
	errorsList := []string{}

	for _, key := range RequiredExtraFields {
		if value, ok := extraMap[key].(string); !ok || value == "" {
			errorsList = append(errorsList, fmt.Sprintf("missing field '%s' in extra mapping", key))
		}
	}

	if deletePolicy, ok := extraMap["deletePolicy"].(string); !ok || ok &&
		deletePolicy != DeletePolicyNone &&
		deletePolicy != DeletePolicyCascade {
		errorsList = append(errorsList, "unknown value 'deletePolicy' in extra mapping")
	}

	itemFamily, ok := extraMap["itemFamily"].(string)
	if !ok {
		errorsList = append(errorsList, "missing field 'itemFamily' in extra mapping")
	}

	valid, familySpecificErrors := validateFamilySpecificFields(extraMap, itemFamily)
	if !valid {
		errorsList = append(errorsList, familySpecificErrors...)
	}

	if len(errorsList) > 0 {
		return nil, fmt.Errorf("invalid extra mapping: %s", strings.Join(errorsList, "; "))
	}

	return extraMap, nil
}

func validateFamilySpecificFields(extraMap map[string]any, itemFamily string) (bool, []string) {
	errorsList := []string{}

	if itemFamily != ExtraRelationshipFamily {
		errorsList = append(errorsList, "unknown value 'itemFamily' in extra mapping")
	}

	if itemFamily == ExtraRelationshipFamily {
		valid, relationshipFamilyErrors := validateRelationshipFamilyFields(extraMap)
		if !valid {
			errorsList = append(errorsList, relationshipFamilyErrors...)
		}
	}
	return len(errorsList) == 0, errorsList
}

func validateRelationshipFamilyFields(extraMap map[string]any) (bool, []string) {
	errorsList := []string{}

	_, ok := extraMap["sourceRef"].(map[string]any)
	if !ok {
		errorsList = append(errorsList, "missing or invalid 'sourceRef' for relationship extra mapping")
	} else {
		sourceRef := extraMap["sourceRef"].(map[string]any)
		if _, ok := sourceRef["apiVersion"].(string); !ok {
			errorsList = append(errorsList, "missing or invalid 'sourceRef.apiVersion' for relationship extra mapping")
		}
		if _, ok := sourceRef["family"].(string); !ok {
			errorsList = append(errorsList, "missing or invalid 'sourceRef.family' for relationship extra mapping")
		}
		if _, ok := sourceRef["name"].(string); !ok {
			errorsList = append(errorsList, "missing or invalid 'sourceRef.name' for relationship extra mapping")
		}
	}

	_, ok = extraMap["typeRef"].(map[string]any)
	if !ok {
		errorsList = append(errorsList, "missing or invalid 'typeRef' for relationship extra mapping")
	} else {
		typeRef := extraMap["typeRef"].(map[string]any)
		if _, ok := typeRef["apiVersion"].(string); !ok {
			errorsList = append(errorsList, "missing or invalid 'typeRef.apiVersion' for relationship extra mapping")
		}
		if _, ok := typeRef["family"].(string); !ok {
			errorsList = append(errorsList, "missing or invalid 'typeRef.family' for relationship extra mapping")
		}
		if _, ok := typeRef["name"].(string); !ok {
			errorsList = append(errorsList, "missing or invalid 'typeRef.name' for relationship extra mapping")
		}
	}

	return len(errorsList) == 0, errorsList
}

// UnmarshalYAML for special handling of the 'extra' field in mapping configurations.
// It validates the presence and correctness of required fields.
func (e *Extra) UnmarshalYAML(value *yaml.Node) error {
	var extraMap map[string]any
	if err := value.Decode(&extraMap); err != nil {
		return err
	}

	extraMap, err := validateExtra(extraMap)
	if err != nil {
		return err
	}

	*e = extraMap
	return nil
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
