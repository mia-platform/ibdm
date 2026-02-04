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
	// ApplyIdentifierExtraTemplate applies only the identifier extra template to the given input data and returns
	ApplyIdentifierExtraTemplate(data map[string]any) ([]ExtraMappedData, error)
}

const (
	maxIdentifierLength       = 253
	extraRelationshipResource = "relationships"
	deletePolicyCascade       = "cascade"
	deletePolicyNone          = "none"
)

var (
	errParsingSpecOutput = errors.New("error during casting to valid object")
	errParsingExtra      = errors.New("error parsing extra templates")

	identifierRegex     = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)
	validExtraResources = []string{extraRelationshipResource}
	validDeletePolicies = []string{deletePolicyNone, deletePolicyCascade}
)

var _ Mapper = &internalMapper{}

// ExtraMapping defines a pre-compiled template for an extra resource.
type ExtraMapping struct {
	APIVersion   string
	Resource     string
	DeletePolicy string
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
	APIVersion   string
	Resource     string
	Identifier   string
	DeletePolicy string
	Spec         map[string]any
}

// ParentResourceInfo holds metadata about the parent resource for relationship extra mappings.
type ParentResourceInfo struct {
	Identifier string
	APIVersion string
	Resource   string
}

// New constructs a Mapper using the provided identifier template and spec templates.
func New(identifierTemplate string, specTemplates map[string]string, extraTemplates []map[string]any) (Mapper, error) {
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

	var extraMappings []ExtraMapping
	if len(extraTemplates) > 0 {
		extraMappings = compileExtraMappings(extraTemplates, tmpl, &parsingErrs)
	}

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

		deletePolicy, _ := extra["deletePolicy"].(string)
		if len(deletePolicy) > 0 && !slices.Contains(validDeletePolicies, deletePolicy) {
			*parsingErrs = errors.Join(*parsingErrs, fmt.Errorf("invalid delete policy: %s", deletePolicy))
			continue
		}
		if deletePolicy == "" {
			deletePolicy = deletePolicyNone
		}

		idStr, _ := extra["identifier"].(string)

		idTmpl, err := tmpl.New("extra-id").Parse(idStr)
		if err != nil {
			*parsingErrs = errors.Join(*parsingErrs, err)
			continue
		}

		bodyMap := make(map[string]any, len(extra))
		for k, v := range extra {
			if k == "resource" || k == "identifier" || k == "apiVersion" || k == "deletePolicy" {
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
			DeletePolicy: deletePolicy,
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

	parentResourceInfo.Identifier = identifier
	extraData, err := executeExtraMappings(data, m.extraMappings, parentResourceInfo)
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

// ApplyIdentifierExtraTemplate implements Mapper.ApplyTemplates.
func (m *internalMapper) ApplyIdentifierExtraTemplate(data map[string]any) ([]ExtraMappedData, error) {
	if len(m.extraMappings) == 0 {
		return nil, nil
	}

	output := make([]ExtraMappedData, 0, len(m.extraMappings))

	for _, extraMapping := range m.extraMappings {
		if extraMapping.DeletePolicy == deletePolicyNone {
			continue
		}

		var idBuf strings.Builder
		if err := extraMapping.IDTemplate.Execute(&idBuf, data); err != nil {
			return nil, fmt.Errorf("%w: %s", errParsingExtra, err.Error())
		}
		identifier := idBuf.String()

		output = append(output, ExtraMappedData{
			APIVersion: extraMapping.APIVersion,
			Resource:   extraMapping.Resource,
			Identifier: identifier,
		})
	}

	return output, nil
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
func executeExtraMappings(data map[string]any, extraMappings []ExtraMapping, parentResourceInfo ParentResourceInfo) ([]ExtraMappedData, error) {
	output := make([]ExtraMappedData, 0, len(extraMappings))

	for _, extraMapping := range extraMappings {
		// Generate Identifier
		var idBuf strings.Builder
		if err := extraMapping.IDTemplate.Execute(&idBuf, data); err != nil {
			return nil, fmt.Errorf("%w: %s", errParsingExtra, err.Error())
		}
		identifier := idBuf.String()

		// Generate Body (Spec)
		var bodyBuf bytes.Buffer
		if err := extraMapping.BodyTemplate.Execute(&bodyBuf, data); err != nil {
			return nil, fmt.Errorf("%w: %s", errParsingExtra, err.Error())
		}

		// Unmarshal the executed YAML back into a map
		var spec map[string]any
		if err := yaml.Unmarshal(bodyBuf.Bytes(), &spec); err != nil {
			return nil, fmt.Errorf("%w: %s", errParsingExtra, err.Error())
		}

		// Handle Special Resources (Relationship)
		if strings.EqualFold(extraMapping.Resource, extraRelationshipResource) {
			spec = enrichRelationshipSpec(spec, parentResourceInfo)
		}

		output = append(output, ExtraMappedData{
			APIVersion: extraMapping.APIVersion,
			Resource:   extraMapping.Resource,
			Identifier: identifier,
			Spec:       spec,
		})
	}

	return output, nil
}

func enrichRelationshipSpec(spec map[string]any, parentResourceInfo ParentResourceInfo) map[string]any {
	// Inject targetRef
	spec["targetRef"] = map[string]any{
		"apiVersion": parentResourceInfo.APIVersion,
		"kind":       parentResourceInfo.Resource,
		"name":       parentResourceInfo.Identifier,
	}

	return spec
}

func IsExtraResourceValid(extraResource string) bool {
	return slices.ContainsFunc(validExtraResources, func(s string) bool {
		return strings.EqualFold(s, extraResource)
	})
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
