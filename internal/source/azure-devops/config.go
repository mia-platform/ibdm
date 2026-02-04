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
	// ErrInvalidEnvVariable reports malformed environment variable values.
	ErrInvalidEnvVariable = errors.New("invalid environment value")
)

// config holds all the configuration needed to connect to Azure DevOps.
type config struct {
	OrganizationURL string `env:"AZURE_DEVOPS_ORGANIZATION_URL"`
	PersonalToken   string `env:"AZURE_DEVOPS_PERSONAL_TOKEN"`
	WebhookPath     string `env:"AZURE_DEVOPS_WEBHOOK_PATH" envDefault:"/azure-devops/webhook"`
	WebhookUser     string `env:"AZURE_DEVOPS_WEBHOOK_USER"`
	WebhookPassword string `env:"AZURE_DEVOPS_WEBHOOK_PASSWORD"`
}

func (c config) validateForSync() error {
	if len(c.OrganizationURL) == 0 {
		return fmt.Errorf("%w: %s", ErrMissingEnvVariable, "AZURE_DEVOPS_ORGANIZATION_URL")
	}

	if len(c.PersonalToken) == 0 {
		return fmt.Errorf("%w: %s", ErrMissingEnvVariable, "AZURE_DEVOPS_PERSONAL_TOKEN")
	}
	return nil
}

func (c config) validateForWebhook() error {
	if len(c.WebhookUser) > 0 && len(c.WebhookPassword) == 0 {
		return fmt.Errorf("%w: %s", ErrInvalidEnvVariable, "if AZURE_DEVOPS_WEBHOOK_USER is set, AZURE_DEVOPS_WEBHOOK_PASSWORD must be set too")
	}

	return nil
}

func (c config) client() (*client, error) {
	return newClient(c.OrganizationURL, c.PersonalToken)
}
