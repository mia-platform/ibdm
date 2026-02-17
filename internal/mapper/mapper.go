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

	"github.com/mia-platform/ibdm/internal/config"
	"github.com/mia-platform/ibdm/internal/mapper/functions"
)

// Mapper renders input data into templated output structures.
// Implementations must provide an identifier template that yields a unique key per entity.
// Template strings are evaluated using Go's text/template engine.
type Mapper interface {
	// ApplyTemplates applies the mapper templates to the given input data and returns the mapped output.
	ApplyTemplates(input map[string]any, parentItemInfo ParentItemInfo) (output MappedData, extra []ExtraMappedData, err error)
	// ApplyIdentifierTemplate applies only the identifier template to the given input data and returns
	ApplyIdentifierTemplate(data map[string]any) (string, []ExtraMappedData, error)
}

const (
	maxIdentifierLength = 253
)

var (
	errParsingSpecOutput = errors.New("error during casting to valid object")
	errParsingExtra      = errors.New("error parsing extra templates")

	identifierRegex = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)
)

var _ Mapper = &internalMapper{}

// ExtraMapping defines a pre-compiled template for an extra item.
type ExtraMapping struct {
	APIVersion       string
	ItemFamily       string
	DeletePolicy     string
	CreateIfTemplate *template.Template
	IDTemplate       *template.Template
	BodyTemplate     *template.Template
}

// internalMapper is the default Mapper implementation backed by text/template.
type internalMapper struct {
	idTemplate       *template.Template
	metadataTemplate *template.Template
	specTemplate     *template.Template
	extraMappings    []ExtraMapping
}

// MappedData wraps the identifier and rendered spec produced by a Mapper.
type MappedData struct {
	Identifier string
	Metadata   map[string]any
	Spec       map[string]any
}

// ExtraMappedData wraps the identifier and rendered spec produced by a Mapper.
type ExtraMappedData struct {
	APIVersion string
	ItemFamily string
	Identifier string
	Spec       map[string]any
}

// ParentItemInfo holds metadata about the parent item for relationship extra mappings.
type ParentItemInfo struct {
	Identifier string
	APIVersion string
	ItemFamily string
}

// New constructs a Mapper using the provided identifier template and spec templates.
func New(identifierTemplate string, metadataTemplates, specTemplates map[string]string, extraTemplates []config.Extra) (Mapper, error) {
	var parsingErrs error
	tmpl := template.New("main").Option("missingkey=error").Funcs(templateFunctions())
	idTemplate, err := tmpl.New("identifier").Parse(identifierTemplate)
	if err != nil {
		parsingErrs = err
	}

	metadataTemplate := compileMetadataTemplates(metadataTemplates, tmpl, &parsingErrs)

	specTemplate := compileSpecTemplates(specTemplates, tmpl, &parsingErrs)

	var extraMappings []ExtraMapping
	if len(extraTemplates) > 0 {
		extraMappings = compileExtraMappings(extraTemplates, tmpl, &parsingErrs)
	}

	if parsingErrs != nil {
		return nil, NewParsingError(parsingErrs)
	}

	return &internalMapper{
		idTemplate:       idTemplate,
		metadataTemplate: metadataTemplate,
		specTemplate:     specTemplate,
		extraMappings:    extraMappings,
	}, nil
}

// compileMetadataTemplates combines the key/value metadata templates into a
// single YAML document template.
func compileMetadataTemplates(metadataTemplates map[string]string, tmpl *template.Template, parsingErrs *error) *template.Template {
	metadataTemplateString := new(strings.Builder)
	metadataTemplateString.WriteString("---\n")
	for key, value := range metadataTemplates {
		metadataTemplateString.WriteString(key + ": " + value + "\n")
	}

	metadataTemplate, err := tmpl.New("metadata").Parse(metadataTemplateString.String())
	if err != nil {
		*parsingErrs = errors.Join(*parsingErrs, err)
	}
	return metadataTemplate
}

// compileSpecTemplates combines the key/value spec templates into a single YAML
// document template.
func compileSpecTemplates(specTemplates map[string]string, tmpl *template.Template, parsingErrs *error) *template.Template {
	specTemplateString := new(strings.Builder)
	specTemplateString.WriteString("---\n")
	for key, value := range specTemplates {
		specTemplateString.WriteString(key + ": " + value + "\n")
	}

	specTemplate, err := tmpl.New("spec").Parse(specTemplateString.String())
	if err != nil {
		*parsingErrs = errors.Join(*parsingErrs, err)
	}
	return specTemplate
}

// compileExtraMappings pre-compiles extra templates (identifier, createIf and body)
// to speed up mapping execution and to catch template errors early.
func compileExtraMappings(extraTemplates []config.Extra, tmpl *template.Template, parsingErrs *error) []ExtraMapping {
	extraMappings := make([]ExtraMapping, 0, len(extraTemplates))
	for _, extra := range extraTemplates {
		// Clone the base template set to avoid name collisions across extras.
		extraTmpl, err := tmpl.Clone()
		if err != nil {
			*parsingErrs = errors.Join(*parsingErrs, err)
			continue
		}

		apiVersion, _ := extra["apiVersion"].(string)
		family, _ := extra["itemFamily"].(string)
		deletePolicy, _ := extra["deletePolicy"].(string)
		idStr, _ := extra["identifier"].(string)
		createIfStr, ok := extra["createIf"].(string)

		var createIfTmpl *template.Template
		if ok && strings.TrimSpace(createIfStr) != "" {
			createIfTmpl, err = extraTmpl.New("extra-createIf").Parse(createIfStr)
			if err != nil {
				*parsingErrs = errors.Join(*parsingErrs, err)
				continue
			}
		}

		idTmpl, err := extraTmpl.New("extra-id").Parse(idStr)
		if err != nil {
			*parsingErrs = errors.Join(*parsingErrs, err)
			continue
		}

		bodyMap := make(map[string]any, len(extra))
		for k, v := range extra {
			if slices.Contains(config.RequiredExtraFields, k) || k == "createIf" {
				continue
			}
			bodyMap[k] = v
		}

		bodyBytes, err := yaml.Marshal(bodyMap)
		if err != nil {
			*parsingErrs = errors.Join(*parsingErrs, err)
			continue
		}

		bodyTmpl, err := extraTmpl.New("extra-body").Parse(string(bodyBytes))
		if err != nil {
			*parsingErrs = errors.Join(*parsingErrs, err)
			continue
		}

		extraMappings = append(extraMappings, ExtraMapping{
			APIVersion:       apiVersion,
			ItemFamily:       family,
			DeletePolicy:     deletePolicy,
			CreateIfTemplate: createIfTmpl,
			IDTemplate:       idTmpl,
			BodyTemplate:     bodyTmpl,
		})
	}
	return extraMappings
}

// ApplyTemplates implements Mapper.ApplyTemplates.
func (m *internalMapper) ApplyTemplates(data map[string]any, parentResourceInfo ParentItemInfo) (MappedData, []ExtraMappedData, error) {
	identifier, err := executeIdentifierTemplate(m.idTemplate, "identifier", data)
	if err != nil {
		return MappedData{}, nil, err
	}

	metadataData, err := executeTemplatesMap(m.metadataTemplate, "metadata", data)
	if err != nil {
		return MappedData{}, nil, err
	}

	specData, err := executeTemplatesMap(m.specTemplate, "spec", data)
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
		Metadata:   metadataData,
		Spec:       specData,
	}, extraData, nil
}

// ApplyIdentifierTemplate implements Mapper.ApplyIdentifierTemplate.
func (m *internalMapper) ApplyIdentifierTemplate(data map[string]any) (string, []ExtraMappedData, error) {
	identifier, err := executeIdentifierTemplate(m.idTemplate, "identifier", data)
	if err != nil {
		return identifier, nil, err
	}

	if len(m.extraMappings) == 0 {
		return identifier, nil, nil
	}

	extras := make([]ExtraMappedData, 0, len(m.extraMappings))

	for _, extraMapping := range m.extraMappings {
		if extraMapping.DeletePolicy == config.DeletePolicyNone {
			continue
		}

		extraIdentifier, err := executeIdentifierTemplate(extraMapping.IDTemplate, "extra-id", data)
		if err != nil {
			return "", nil, err
		}

		extras = append(extras, ExtraMappedData{
			APIVersion: extraMapping.APIVersion,
			ItemFamily: extraMapping.ItemFamily,
			Identifier: extraIdentifier,
		})
	}

	return identifier, extras, nil
}

// executeIdentifierTemplate renders the identifier template with data and validates the result.
func executeIdentifierTemplate(tmpl *template.Template, name string, data map[string]any) (string, error) {
	outputStrBuilder := new(strings.Builder)
	err := tmpl.ExecuteTemplate(outputStrBuilder, name, data)
	generatedID := outputStrBuilder.String()

	if !identifierRegex.MatchString(generatedID) || len(generatedID) > maxIdentifierLength {
		return "", template.ExecError{
			Name: name,
			Err:  fmt.Errorf("template: %s: generated identifier '%s' is invalid; it can contain only lowercase alphanumeric characters, '-' or '.', must start and finish with an alphanumeric character and it can contain no more than %d characters", name, generatedID, maxIdentifierLength),
		}
	}

	return generatedID, err
}

// executeTemplatesMap renders the spec template and converts it into a map.
func executeTemplatesMap(templates *template.Template, templateName string, data map[string]any) (map[string]any, error) {
	output := make(map[string]any)
	outputBuilder := new(bytes.Buffer)
	err := templates.ExecuteTemplate(outputBuilder, templateName, data)
	if err != nil {
		return nil, err
	}

	if err := yaml.Unmarshal(outputBuilder.Bytes(), output); err != nil {
		return nil, fmt.Errorf("%w: %s", errParsingSpecOutput, err.Error())
	}
	return output, nil
}

// executeExtraCreateIfTemplate renders an extra mapping "createIf" template and
// parses its result into a boolean.
func executeExtraCreateIfTemplate(data map[string]any, extraMapping ExtraMapping) (bool, error) {
	// Generate CreateIf
	var createIfBuf bytes.Buffer
	if err := extraMapping.CreateIfTemplate.Execute(&createIfBuf, data); err != nil {
		return false, fmt.Errorf("%w: %s", errParsingExtra, err.Error())
	}

	// Unmarshal the executed YAML back into a map
	var createIf bool
	if err := yaml.Unmarshal(createIfBuf.Bytes(), &createIf); err != nil {
		return false, fmt.Errorf("%w: %s", errParsingExtra, err.Error())
	}
	return createIf, nil
}

// executeExtraMappings renders the pre-compiled extra templates.
func executeExtraMappings(data map[string]any, extraMappings []ExtraMapping, parentResourceInfo ParentItemInfo) ([]ExtraMappedData, error) {
	output := make([]ExtraMappedData, 0, len(extraMappings))

	for _, extraMapping := range extraMappings {
		if extraMapping.CreateIfTemplate != nil {
			createIf, err := executeExtraCreateIfTemplate(data, extraMapping)
			if err != nil {
				return nil, err
			}

			if !createIf {
				continue
			}
		}

		// Generate Identifier
		identifier, err := executeIdentifierTemplate(extraMapping.IDTemplate, "extra-id", data)
		if err != nil {
			return nil, fmt.Errorf("%w: %s", errParsingExtra, err.Error())
		}

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

		// Handle Special Resources
		spec = enrichSpec(spec, parentResourceInfo, extraMapping.ItemFamily)

		output = append(output, ExtraMappedData{
			APIVersion: extraMapping.APIVersion,
			ItemFamily: extraMapping.ItemFamily,
			Identifier: identifier,
			Spec:       spec,
		})
	}

	return output, nil
}

// enrichSpec applies special-case mutations to a rendered extra spec based on
// the extra item family.
func enrichSpec(spec map[string]any, parentResourceInfo ParentItemInfo, itemFamily string) map[string]any {
	if strings.EqualFold(itemFamily, config.ExtraRelationshipFamily) {
		spec = enrichRelationshipSpec(spec, parentResourceInfo)
	}

	return spec
}

func enrichRelationshipSpec(spec map[string]any, parentResourceInfo ParentItemInfo) map[string]any {
	// Inject targetRef
	spec["targetRef"] = map[string]any{
		"apiVersion": parentResourceInfo.APIVersion,
		"family":     parentResourceInfo.ItemFamily,
		"name":       parentResourceInfo.Identifier,
	}

	return spec
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
