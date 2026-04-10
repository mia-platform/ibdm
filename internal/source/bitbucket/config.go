// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package bitbucket

import (
	"fmt"
	"time"

	"github.com/caarlos0/env/v11"
)

// sourceConfig holds the environment-driven Bitbucket API settings.
type sourceConfig struct {
	URL         string        `env:"BITBUCKET_URL"          envDefault:"https://api.bitbucket.org"`
	AccessToken string        `env:"BITBUCKET_ACCESS_TOKEN"`
	APIUsername string        `env:"BITBUCKET_API_USERNAME"`
	APIToken    string        `env:"BITBUCKET_API_TOKEN"`
	HTTPTimeout time.Duration `env:"BITBUCKET_HTTP_TIMEOUT"  envDefault:"30s"`
	Workspace   string        `env:"BITBUCKET_WORKSPACE"`
}

// webhookConfig holds the environment-driven Bitbucket webhook settings.
type webhookConfig struct {
	WebhookSecret string `env:"BITBUCKET_WEBHOOK_SECRET"`
	WebhookPath   string `env:"BITBUCKET_WEBHOOK_PATH"   envDefault:"/bitbucket/webhook"`
}

// loadSourceConfigFromEnv parses BITBUCKET_* environment variables into a sourceConfig.
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

// validate checks that the authentication configuration is internally consistent.
func (c sourceConfig) validate() error {
	hasAccessToken := c.AccessToken != ""
	hasBasicAuth := c.APIUsername != "" || c.APIToken != ""

	switch {
	case !hasAccessToken && !hasBasicAuth:
		return fmt.Errorf("%w: one of BITBUCKET_ACCESS_TOKEN or BITBUCKET_API_USERNAME/BITBUCKET_API_TOKEN must be set",
			ErrMissingEnvVariable)
	case hasAccessToken && hasBasicAuth:
		return fmt.Errorf("%w: BITBUCKET_ACCESS_TOKEN and BITBUCKET_API_USERNAME/BITBUCKET_API_TOKEN are mutually exclusive",
			ErrInvalidEnvVariable)
	case hasBasicAuth && (c.APIUsername == "" || c.APIToken == ""):
		return fmt.Errorf("%w: BITBUCKET_API_USERNAME and BITBUCKET_API_TOKEN must both be set for basic auth",
			ErrMissingEnvVariable)
	}

	return nil
}

// loadWebhookConfigFromEnv parses BITBUCKET_WEBHOOK_* environment variables into a webhookConfig.
func loadWebhookConfigFromEnv() (webhookConfig, error) {
	cfg, err := env.ParseAs[webhookConfig]()
	if err != nil {
		return webhookConfig{}, err
	}
	return cfg, nil
}
