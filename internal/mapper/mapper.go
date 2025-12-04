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

// Mapper will define how to map input data to an output structure defined by its Templates.
// Identifier is a special fields used to uniquely identify an entity and is required.
// All the string values will be used as go string templates to generate its value from the input data.
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

// internalMapper is the default implementation of the Mapper interface.
type internalMapper struct {
	idTemplate   *template.Template
	specTemplate *template.Template
}

// MappedData contains the result of applying a Mapper to some input data.
type MappedData struct {
	Identifier string
	Spec       map[string]any
}

// New creates a new Mapper with the given identifier template and a specTemplates map.
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

// templateFunctions returns the custom functions available for the user templates in addition
// to the default ones provided by the text/template package.
func templateFunctions() template.FuncMap {
	return template.FuncMap{
		// string functions
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
		"toJSON": functions.ToJSON,
		"pluck":  functions.Pluck,
		"get":    functions.Get,

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

// executeIdentifierTemplate executes a text/template named "identifier" with the given data.
// Return the result as a string.
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
