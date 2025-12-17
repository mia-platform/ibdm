// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package mapper

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
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
	ApplyTemplates(input map[string]any) (output MappedData, err error)
}

const (
	maxIdentifierLength = 253
)

var (
	errParsingSpecOutput = errors.New("error during casting to valid object")

	identifierRegex = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)
)

var _ Mapper = &internalMapper{}

// internalMapper is the default Mapper implementation backed by text/template.
type internalMapper struct {
	idTemplate   *template.Template
	specTemplate *template.Template
}

// MappedData wraps the identifier and rendered spec produced by a Mapper.
type MappedData struct {
	Identifier string
	Spec       map[string]any
}

// New constructs a Mapper using the provided identifier template and spec templates.
func New(identifierTemplate string, specTemplates map[string]string) (Mapper, error) {
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

	if parsingErrs != nil {
		return nil, NewParsingError(parsingErrs)
	}

	return &internalMapper{
		idTemplate:   idTemplate,
		specTemplate: specTemplate,
	}, nil
}

// ApplyTemplates applies the mapper templates to the given input data and returns the mapped output.
func (m *internalMapper) ApplyTemplates(data map[string]any) (MappedData, error) {
	identifier, err := executeIdentifierTemplate(m.idTemplate, data)
	if err != nil {
		return MappedData{}, err
	}

	specData, err := executeTemplatesMap(m.specTemplate, data)
	if err != nil {
		return MappedData{}, err
	}

	return MappedData{
		Identifier: identifier,
		Spec:       specData,
	}, nil
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
