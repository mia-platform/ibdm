// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package azuredevops

import (
	"errors"
	"fmt"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7"
)

var (
	// ErrMissingEnvVariable reports missing mandatory environment variables.
	ErrMissingEnvVariable = errors.New("missing environment variable")
)

// config holds all the configuration needed to connect to Azure DevOps.
type config struct {
	OrganizationURL string `env:"AZURE_DEVOPS_ORGANIZATION_URL"`
	PersonalToken   string `env:"AZURE_DEVOPS_PERSONAL_TOKEN"`
}

func (c config) validate() error {
	if len(c.OrganizationURL) == 0 {
		return fmt.Errorf("%w: %s", ErrMissingEnvVariable, "AZURE_DEVOPS_ORGANIZATION")
	}

	if len(c.PersonalToken) == 0 {
		return fmt.Errorf("%w: %s", ErrMissingEnvVariable, "AZURE_DEVOPS_PERSONAL_TOKEN")
	}
	return nil
}

func (c config) connection() *azuredevops.Connection {
	return azuredevops.NewPatConnection(c.OrganizationURL, c.PersonalToken)
}
