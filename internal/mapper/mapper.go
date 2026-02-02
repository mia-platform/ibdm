// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package mapper

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"

	"github.com/mia-platform/ibdm/internal/mapper/functions"
)

// Mapper renders input data into templated output structures.
// Implementations must provide an identifier template that yields a unique key per entity.
// Template strings are evaluated using Go's text/template engine.
type Mapper interface {
	// ApplyTemplates applies the mapper templates to the given input data and returns the mapped output.
	ApplyTemplates(input map[string]any, parentResourceInfo ParentResourceInfo) (output MappedData, extra []ExtraMappedData, err error)
	// ApplyIdentifierTemplate applies only the identifier template to the given input data and returns
	ApplyIdentifierTemplate(data map[string]any) (string, error)
}

const (
	maxIdentifierLength       = 253
	extraRelationshipResource = "relationships"
)

var (
	errParsingSpecOutput = errors.New("error during casting to valid object")
	errParsingExtra      = errors.New("error parsing extra templates")

	identifierRegex     = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)
	validExtraResources = []string{extraRelationshipResource}
)

var _ Mapper = &internalMapper{}

// ExtraMapping defines a pre-compiled template for an extra resource.
type ExtraMapping struct {
	APIVersion   string
	Resource     string
	IDTemplate   *template.Template
	BodyTemplate *template.Template
}

// internalMapper is the default Mapper implementation backed by text/template.
type internalMapper struct {
	idTemplate    *template.Template
	specTemplate  *template.Template
	extraMappings []ExtraMapping
}

// MappedData wraps the identifier and rendered spec produced by a Mapper.
type MappedData struct {
	Identifier string
	Spec       map[string]any
}

// ExtraMappedData wraps the identifier and rendered spec produced by a Mapper.
type ExtraMappedData struct {
	APIVersion string
	Resource   string
	Identifier string
	Spec       map[string]any
}

// ParentResourceInfo holds metadata about the parent resource for relationship extra mappings.
type ParentResourceInfo struct {
	ParentAPIVersion string
	ParentResource   string
}

// New constructs a Mapper using the provided identifier template and spec templates.
func New(apiVersion, resource, identifierTemplate string, specTemplates map[string]string, extraTemplates []map[string]any) (Mapper, error) {
	var parsingErrs error
	tmpl := template.New("main").Option("missingkey=error").Funcs(templateFunctions())
	idTemplate, err := tmpl.New("identifier").Parse(identifierTemplate)
	if err != nil {
		parsingErrs = err
	}

	specTemplateString := new(strings.Builder)
	specTemplateString.WriteString("---\n")
	for key, value := range specTemplates {
		specTemplateString.WriteString(key + ": " + value + "\n")
	}

	specTemplate, err := tmpl.New("spec").Parse(specTemplateString.String())
	if err != nil {
		parsingErrs = errors.Join(parsingErrs, err)
	}

	extraMappings := compileExtraMappings(extraTemplates, tmpl, &parsingErrs)

	if parsingErrs != nil {
		return nil, NewParsingError(parsingErrs)
	}

	return &internalMapper{
		idTemplate:    idTemplate,
		specTemplate:  specTemplate,
		extraMappings: extraMappings,
	}, nil
}

func compileExtraMappings(extraTemplates []map[string]any, tmpl *template.Template, parsingErrs *error) []ExtraMapping {
	extraMappings := make([]ExtraMapping, 0, len(extraTemplates))
	for _, extra := range extraTemplates {
		apiVersion, _ := extra["apiVersion"].(string)
		resource, _ := extra["resource"].(string)
		ok := IsExtraResourceValid(resource)
		if !ok {
			*parsingErrs = errors.Join(*parsingErrs, fmt.Errorf("invalid extra resource: %s", resource))
			continue
		}

		idStr, _ := extra["identifier"].(string)

		idTmpl, err := tmpl.New("extra-id").Parse(idStr)
		if err != nil {
			*parsingErrs = errors.Join(*parsingErrs, err)
			continue
		}

		bodyMap := make(map[string]any, len(extra))
		for k, v := range extra {
			if k == "resource" || k == "identifier" || k == "apiVersion" {
				continue
			}
			bodyMap[k] = v
		}

		bodyBytes, err := yaml.Marshal(bodyMap)
		if err != nil {
			*parsingErrs = errors.Join(*parsingErrs, err)
			continue
		}

		bodyTmpl, err := tmpl.New("extra-body").Parse(string(bodyBytes))
		if err != nil {
			*parsingErrs = errors.Join(*parsingErrs, err)
			continue
		}

		extraMappings = append(extraMappings, ExtraMapping{
			APIVersion:   apiVersion,
			Resource:     resource,
			IDTemplate:   idTmpl,
			BodyTemplate: bodyTmpl,
		})
	}
	return extraMappings
}

// ApplyTemplates implements Mapper.ApplyTemplates.
func (m *internalMapper) ApplyTemplates(data map[string]any, parentResourceInfo ParentResourceInfo) (MappedData, []ExtraMappedData, error) {
	identifier, err := executeIdentifierTemplate(m.idTemplate, data)
	if err != nil {
		return MappedData{}, nil, err
	}

	specData, err := executeTemplatesMap(m.specTemplate, data)
	if err != nil {
		return MappedData{}, nil, err
	}

	extraData, err := executeExtraMappings(data, identifier, m.extraMappings, parentResourceInfo)
	if err != nil {
		return MappedData{}, nil, err
	}

	return MappedData{
		Identifier: identifier,
		Spec:       specData,
	}, extraData, nil
}

// ApplyIdentifierTemplate implements Mapper.ApplyTemplates.
func (m *internalMapper) ApplyIdentifierTemplate(data map[string]any) (string, error) {
	return executeIdentifierTemplate(m.idTemplate, data)
}

// templateFunctions exposes the custom helpers added to every mapping template.
func templateFunctions() template.FuncMap {
	return template.FuncMap{
		// string functions
		"quote":      functions.Quote,
		"trim":       functions.TrimSpace,
		"trimPrefix": functions.TrimPrefix,
		"trimSuffix": functions.TrimSuffix,
		"replace":    functions.Replace,
		"upper":      functions.ToUpper,
		"lower":      functions.ToLower,
		"truncate":   functions.Truncate,
		"split":      functions.Split,
		"encode64":   functions.EncodeBase64,
		"decode64":   functions.DecodeBase64,

		// object functions
		"object": functions.Object,
		"toJSON": functions.ToJSON,
		"pick":   functions.Pick,
		"get":    functions.Get,
		"set":    functions.Set,

		// list functions
		"list":    functions.List,
		"append":  functions.Append,
		"prepend": functions.Prepend,
		"first":   functions.First,
		"last":    functions.Last,

		// time functions
		"now": functions.Now,

		// cryptographic functions
		"sha256sum": functions.Sha256Sum,
		"sha512sum": functions.Sha512Sum,

		// uuid functions
		"uuidv4": functions.UUIDV4,
		"uuidv6": functions.UUIDV6,
		"uuidv7": functions.UUIDV7,
	}
}

// executeIdentifierTemplate renders the identifier template with data and validates the result.
func executeIdentifierTemplate(tmpl *template.Template, data map[string]any) (string, error) {
	outputStrBuilder := new(strings.Builder)
	err := tmpl.ExecuteTemplate(outputStrBuilder, "identifier", data)
	generatedID := outputStrBuilder.String()

	if !identifierRegex.MatchString(generatedID) || len(generatedID) > maxIdentifierLength {
		return "", template.ExecError{
			Name: "identifier",
			Err:  fmt.Errorf("template: identifier: generated identifier '%s' is invalid; it can contain only lowercase alphanumeric characters, '-' or '.', must start and finish with an alphanumeric character and it can contain no more than %d characters", generatedID, maxIdentifierLength),
		}
	}

	return generatedID, err
}

// executeTemplatesMap renders the spec template and converts it into a map.
func executeTemplatesMap(templates *template.Template, data map[string]any) (map[string]any, error) {
	output := make(map[string]any)
	outputBuilder := new(bytes.Buffer)
	err := templates.ExecuteTemplate(outputBuilder, "spec", data)
	if err != nil {
		return nil, err
	}

	if err := yaml.Unmarshal(outputBuilder.Bytes(), output); err != nil {
		return nil, fmt.Errorf("%w: %s", errParsingSpecOutput, err.Error())
	}
	return output, nil
}

// executeExtraMappings renders the pre-compiled extra templates.
func executeExtraMappings(data map[string]any, parentIdentifier string, extraMappings []ExtraMapping, parentResourceInfo ParentResourceInfo) ([]ExtraMappedData, error) {
	output := make([]ExtraMappedData, 0, len(extraMappings))

	for _, mapping := range extraMappings {
		// Generate Identifier
		var idBuf strings.Builder
		if err := mapping.IDTemplate.Execute(&idBuf, data); err != nil {
			return nil, err
		}
		identifier := idBuf.String()

		// Generate Body (Spec)
		var bodyBuf bytes.Buffer
		if err := mapping.BodyTemplate.Execute(&bodyBuf, data); err != nil {
			return nil, err
		}

		// Unmarshal the executed YAML back into a map
		var spec map[string]any
		if err := yaml.Unmarshal(bodyBuf.Bytes(), &spec); err != nil {
			return nil, fmt.Errorf("error parsing extra spec: %w", err)
		}

		// Handle Special Resources (Relationship)
		if strings.EqualFold(mapping.Resource, extraRelationshipResource) {
			spec = enrichRelationshipSpec(spec, parentIdentifier, parentResourceInfo.ParentAPIVersion, parentResourceInfo.ParentResource)
		}

		output = append(output, ExtraMappedData{
			APIVersion: mapping.APIVersion,
			Resource:   mapping.Resource,
			Identifier: identifier,
			Spec:       spec,
		})
	}

	return output, nil
}

func enrichRelationshipSpec(spec map[string]any, parentIdentifier, apiVersion, resource string) map[string]any {
	// Inject targetRef
	spec["targetRef"] = map[string]any{
		"apiVersion": apiVersion,
		"resource":   resource,
		"name":       parentIdentifier,
	}

	return spec
}

func IsExtraResourceValid(extraResource string) bool {
	return slices.ContainsFunc(validExtraResources, func(s string) bool {
		return strings.EqualFold(s, extraResource)
	})
}
