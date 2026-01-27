// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package azuredevops

import (
	"errors"
	"fmt"
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

func (c config) client() (*client, error) {
	return newClient(c.OrganizationURL, c.PersonalToken)
}
