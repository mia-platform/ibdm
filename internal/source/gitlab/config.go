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
	BaseURL string `env:"GITLAB_BASE_URL,required"`
}

// webhookConfig holds the environment-driven GitLab webhook settings.
type webhookConfig struct {
	WebhookPath  string `env:"GITLAB_WEBHOOK_PATH" envDefault:"/gitlab/webhook"`
	WebhookToken string `env:"GITLAB_WEBHOOK_TOKEN"`
}

const (
	msgMissingBaseURL     = "GITLAB_BASE_URL is required"
	msgInvalidWebhookPath = "GITLAB_WEBHOOK_PATH must start with '/'"
)

var (
	// ErrSourceConfigNotValid is returned when the source configuration is invalid.
	ErrSourceConfigNotValid = errors.New("gitlab source configuration not valid")
	// ErrWebhookConfigNotValid is returned when the webhook configuration is invalid.
	ErrWebhookConfigNotValid = errors.New("gitlab webhook configuration not valid")
)

// loadSourceConfigFromEnv parses GITLAB_* environment variables into a sourceConfig.
func loadSourceConfigFromEnv() (sourceConfig, error) {
	cfg, err := env.ParseAs[sourceConfig]()
	if err != nil {
		return sourceConfig{}, err
	}

	if err := cfg.validate(); err != nil {
		return sourceConfig{}, err
	}

	return cfg, nil
}

// validate checks that the source configuration is internally consistent.
func (c sourceConfig) validate() error {
	if c.BaseURL == "" {
		return fmt.Errorf("%w: %s", ErrSourceConfigNotValid, msgMissingBaseURL)
	}

	return nil
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
