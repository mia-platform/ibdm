// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
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
	IdentifierField   = "identifier"
	ItemFamilyField   = "itemFamily"
	NameField         = "name"
	SourceRefField    = "sourceRef"
	TargetRefField    = "targetRef"
	TypeField         = "type"
	TypeRefField      = "typeRef"

	// maxMappingNameLength caps the length of a mapping name.
	maxMappingNameLength = 63
)

var (
	// ErrParsing reports failures that occur while decoding mapping files.
	ErrParsing = errors.New("error parsing")
	// ErrDuplicateMappingName reports two mappings sharing a name in one loaded set.
	ErrDuplicateMappingName = errors.New("duplicate mapping name")

	RequiredExtraFields = []string{APIVersionField, ItemFamilyField, DeletePolicyField, IdentifierField}

	// mappingNameRegex accepts the shapes already used as mapping file base names.
	mappingNameRegex = regexp.MustCompile(`^[a-z0-9]([a-z0-9._-]*[a-z0-9])?$`)
)

// MappingConfig holds the configuration for mapping rules.
type MappingConfig struct {
	// Name identifies the mapping within the loaded set. It defaults to the base
	// name of the file the mapping was read from.
	Name       string         `json:"name" yaml:"name"`
	Type       string         `json:"type" yaml:"type"`
	Extra      map[string]any `json:"extra,omitempty" yaml:"extra,omitempty"`
	APIVersion string         `json:"apiVersion" yaml:"apiVersion"`
	ItemFamily string         `json:"itemFamily" yaml:"itemFamily"`
	Syncable   bool           `json:"syncable" yaml:"syncable"`
	Mappings   Mappings       `json:"mappings" yaml:"mappings"`

	// path is the file the mapping was read from, used to report name collisions.
	path string
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
	Owner             string `json:"owner,omitempty" yaml:"owner,omitempty"`
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

	for _, field := range []string{SourceRefField, TargetRefField, TypeRefField} {
		if value, ok := extraMap[field].(string); !ok || value == "" {
			errorsList = append(errorsList, fmt.Sprintf("missing or invalid '%s' for relationship extra mapping", field))
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

		config.path = path
		configs = append(configs, config)
	}

	if err := resolveMappingNames(path, configs); err != nil {
		return nil, err
	}

	return configs, nil
}

// resolveMappingNames defaults the name of a single-mapping file to the file base
// name, requires an explicit name on every mapping of a multi-mapping file, and
// validates every resulting name.
func resolveMappingNames(path string, configs []*MappingConfig) error {
	if len(configs) == 1 && configs[0].Name == "" {
		configs[0].Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}

	for _, config := range configs {
		if config.Name == "" {
			return fmt.Errorf("%w %q: field %q is required when a file contains more than one mapping", ErrParsing, path, NameField)
		}

		if err := validateMappingName(config.Name); err != nil {
			return fmt.Errorf("%w %q: %w", ErrParsing, path, err)
		}
	}

	return nil
}

// validateMappingName checks a mapping name against the allowed charset and length.
func validateMappingName(name string) error {
	if len(name) > maxMappingNameLength {
		return fmt.Errorf("invalid mapping name %q: longer than %d characters", name, maxMappingNameLength)
	}

	if !mappingNameRegex.MatchString(name) {
		return fmt.Errorf("invalid mapping name %q: must consist of lowercase alphanumeric characters, '.', '_' or '-', "+
			"and must start and end with an alphanumeric character; set the %q field explicitly if the file name does not comply",
			name, NameField)
	}

	return nil
}

// ValidateMappingNames reports mappings that share a name. Source emissions can
// target mappings by name, so duplicates would make an emission ambiguous.
func ValidateMappingNames(mappings []*MappingConfig) error {
	seen := make(map[string]*MappingConfig, len(mappings))
	for _, mapping := range mappings {
		if previous, found := seen[mapping.Name]; found {
			return fmt.Errorf("%w %q: defined in %q and %q", ErrDuplicateMappingName, mapping.Name, previous.path, mapping.path)
		}

		seen[mapping.Name] = mapping
	}

	return nil
}
