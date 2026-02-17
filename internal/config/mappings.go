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
	// DeletePolicyCascade indicates that related items should be deleted when the source item is deleted.
	DeletePolicyCascade = "cascade"
	// DeletePolicyNone indicates that related items should not be deleted when the source item is deleted.
	DeletePolicyNone = "none"

	// ExtraRelationshipFamily indicates that the mapping is for relationships between items.
	ExtraRelationshipFamily = "relationships"

	APIVersionField   = "apiVersion"
	DeletePolicyField = "deletePolicy"
	FamilyField       = "family"
	IdentifierField   = "identifier"
	ItemFamilyField   = "itemFamily"
	NameField         = "name"
	SourceRefField    = "sourceRef"
	TypeField         = "type"
	TypeRefField      = "typeRef"
)

var (
	// ErrParsing reports failures that occur while decoding mapping files.
	ErrParsing = errors.New("error parsing")

	RequiredExtraFields = []string{APIVersionField, ItemFamilyField, DeletePolicyField, IdentifierField}
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

// MetadataMapping holds a flattened representation of metadata templates.
type MetadataMapping map[string]string

// MetadataTemplate is the strongly-typed representation of the metadata section
// in mapping files.
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

// UnmarshalYAML decodes YAML metadata templates and flattens them into a map.
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

// Extra holds an extra mapping definition as a generic map after validation.
type Extra map[string]any

// validateExtra validates the required fields and domain-specific constraints
// of an extra mapping.
func validateExtra(extraMap map[string]any) (map[string]any, error) {
	errorsList := []string{}

	for _, key := range RequiredExtraFields {
		if value, ok := extraMap[key].(string); !ok || value == "" {
			errorsList = append(errorsList, fmt.Sprintf("missing field '%s' in extra mapping", key))
		}
	}

	if deletePolicy, ok := extraMap[DeletePolicyField].(string); !ok || ok &&
		deletePolicy != DeletePolicyNone &&
		deletePolicy != DeletePolicyCascade {
		errorsList = append(errorsList, fmt.Sprintf("unknown value '%s' in extra mapping", DeletePolicyField))
	}

	itemFamily, ok := extraMap[ItemFamilyField].(string)
	if !ok {
		errorsList = append(errorsList, fmt.Sprintf("missing field '%s' in extra mapping", ItemFamilyField))
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

// validateFamilySpecificFields validates fields that depend on the configured
// extra item family.
func validateFamilySpecificFields(extraMap map[string]any, itemFamily string) (bool, []string) {
	errorsList := []string{}

	if itemFamily != ExtraRelationshipFamily {
		errorsList = append(errorsList, fmt.Sprintf("unknown value '%s' in extra mapping", ItemFamilyField))
	}

	if itemFamily == ExtraRelationshipFamily {
		valid, relationshipFamilyErrors := validateRelationshipFamilyFields(extraMap)
		if !valid {
			errorsList = append(errorsList, relationshipFamilyErrors...)
		}
	}
	return len(errorsList) == 0, errorsList
}

// validateRelationshipFamilyFields validates the relationship-specific fields
// in an extra mapping.
func validateRelationshipFamilyFields(extraMap map[string]any) (bool, []string) {
	errorsList := []string{}

	sourceRef, ok := extraMap[SourceRefField].(map[string]any)
	if !ok {
		errorsList = append(errorsList, fmt.Sprintf("missing or invalid '%s' for relationship extra mapping", SourceRefField))
	} else {
		if apiVersionField, ok := sourceRef[APIVersionField].(string); !ok || apiVersionField == "" {
			errorsList = append(errorsList, fmt.Sprintf("missing or invalid '%s.%s' for relationship extra mapping", SourceRefField, APIVersionField))
		}
		if familyField, ok := sourceRef[FamilyField].(string); !ok || familyField == "" {
			errorsList = append(errorsList, fmt.Sprintf("missing or invalid '%s.%s' for relationship extra mapping", SourceRefField, FamilyField))
		}
		if nameField, ok := sourceRef[NameField].(string); !ok || nameField == "" {
			errorsList = append(errorsList, fmt.Sprintf("missing or invalid '%s.%s' for relationship extra mapping", SourceRefField, NameField))
		}
	}

	typeRef, ok := extraMap[TypeRefField].(map[string]any)
	if !ok {
		errorsList = append(errorsList, fmt.Sprintf("missing or invalid '%s' for relationship extra mapping", TypeRefField))
	} else {
		if apiVersionField, ok := typeRef[APIVersionField].(string); !ok || apiVersionField == "" {
			errorsList = append(errorsList, fmt.Sprintf("missing or invalid '%s.%s' for relationship extra mapping", TypeRefField, APIVersionField))
		}
		if familyField, ok := typeRef[FamilyField].(string); !ok || familyField == "" {
			errorsList = append(errorsList, fmt.Sprintf("missing or invalid '%s.%s' for relationship extra mapping", TypeRefField, FamilyField))
		}
		if nameField, ok := typeRef[NameField].(string); !ok || nameField == "" {
			errorsList = append(errorsList, fmt.Sprintf("missing or invalid '%s.%s' for relationship extra mapping", TypeRefField, NameField))
		}
	}

	return len(errorsList) == 0, errorsList
}

// UnmarshalYAML decodes YAML for an extra mapping and validates required fields.
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
			missingFields = append(missingFields, TypeField)
		}
		if config.APIVersion == "" {
			missingFields = append(missingFields, APIVersionField)
		}
		if config.ItemFamily == "" {
			missingFields = append(missingFields, ItemFamilyField)
		}

		if len(missingFields) > 0 {
			return nil, fmt.Errorf("%w %q: missing required fields: %v", ErrParsing, path, strings.Join(missingFields, ", "))
		}

		configs = append(configs, config)
	}

	return configs, nil
}
