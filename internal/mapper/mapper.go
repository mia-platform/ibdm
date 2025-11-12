// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package mapper

import (
	"errors"
	"text/template"

	"github.com/mia-platform/ibdm/internal/mapper/functions"
)

// Mapper will define how to map input data to an output structure defined by its Templates.
// Identifier is a special fields used to uniquely identify an entity and is required.
// All the string values will be used as go string templates to generate its value from the input data.
type Mapper interface {
	// ApplyTemplates applies the mapper templates to the given input data and returns the mapped output.
	ApplyTemplates(input map[string]any) (output MappedData, err error)
}

var _ Mapper = &internalMapper{}

// internalMapper is the default implementation of the Mapper interface.
type internalMapper struct {
	idTemplate *template.Template
	templates  map[string]*template.Template
}

// MappedData contains the result of applying a Mapper to some input data.
type MappedData struct {
	Identifier string
	Spec       map[string]any
}

// New creates a new Mapper with the given identifier template and a specTemplates map.
func New(identifierTemplate string, specTemplates map[string]string) (Mapper, error) {
	var parsingErrs error
	tmpl := template.New("main").Option("missingkey=zero").Funcs(templateFunctions())
	idTemplate, err := tmpl.New("identifier").Parse(identifierTemplate)
	if err != nil {
		parsingErrs = err
	}

	templates := make(map[string]*template.Template, len(specTemplates))
	for key, value := range specTemplates {
		templates[key], err = tmpl.New(key).Parse(value)
		if err != nil {
			parsingErrs = errors.Join(parsingErrs, err)
		}
	}

	if parsingErrs != nil {
		return nil, NewParsingError(parsingErrs)
	}

	return &internalMapper{
		idTemplate: idTemplate,
		templates:  templates,
	}, nil
}

// ApplyTemplates applies the mapper templates to the given input data and returns the mapped output.
func (m *internalMapper) ApplyTemplates(_ map[string]any) (MappedData, error) {
	return MappedData{}, nil
}

// templateFunctions returns the custom functions available for the user templates in addition
// to the default ones provided by the text/template package.
func templateFunctions() template.FuncMap {
	return template.FuncMap{
		// string functions
		"trim":  functions.TrimSpace,
		"upper": functions.ToUpper,
		"lower": functions.ToLower,

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
