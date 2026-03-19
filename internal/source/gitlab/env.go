// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package gitlab

import (
	"errors"
	"fmt"
	"strings"

	"github.com/caarlos0/env/v11"
)

// sourceConfig holds the environment-driven GitLab API settings.
type sourceConfig struct {
	Token   string `env:"GITLAB_TOKEN,required"`
	BaseURL string `env:"GITLAB_BASE_URL" envDefault:"https://gitlab.com"`
}

// webhookConfig holds the environment-driven GitLab webhook settings.
type webhookConfig struct {
	WebhookPath  string `env:"GITLAB_WEBHOOK_PATH" envDefault:"/gitlab/webhook"`
	WebhookToken string `env:"GITLAB_WEBHOOK_TOKEN"`
}

const (
	msgInvalidWebhookPath = "GITLAB_WEBHOOK_PATH must start with '/'"
)

var (
	// ErrWebhookConfigNotValid is returned when the webhook configuration is invalid.
	ErrWebhookConfigNotValid = errors.New("gitlab webhook configuration not valid")
)

// loadSourceConfigFromEnv parses GITLAB_* environment variables into a sourceConfig.
func loadSourceConfigFromEnv() (sourceConfig, error) {
	return env.ParseAs[sourceConfig]()
}

// loadWebhookConfigFromEnv parses GITLAB_WEBHOOK_* environment variables into a
// webhookConfig and validates the result.
func loadWebhookConfigFromEnv() (webhookConfig, error) {
	cfg, err := env.ParseAs[webhookConfig]()
	if err != nil {
		return webhookConfig{}, err
	}

	if err := cfg.validate(); err != nil {
		return webhookConfig{}, err
	}

	return cfg, nil
}

// validate checks that the webhook configuration is internally consistent.
func (c webhookConfig) validate() error {
	if !strings.HasPrefix(c.WebhookPath, "/") {
		return fmt.Errorf("%w: %s", ErrWebhookConfigNotValid, msgInvalidWebhookPath)
	}

	return nil
}
