// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package github

import (
	"errors"
	"fmt"
	"time"

	"github.com/caarlos0/env/v11"
)

const (
	// githubMaxPageSize is the upper bound for GITHUB_PAGE_SIZE.
	// The GitHub REST API enforces a maximum of 100 items per page.
	githubMaxPageSize = 100
)

var (
	// ErrMissingEnvVariable reports missing mandatory environment variables.
	ErrMissingEnvVariable = errors.New("missing environment variable")
	// ErrInvalidEnvVariable reports malformed environment variable values.
	ErrInvalidEnvVariable = errors.New("invalid environment value")
)

// config holds the environment-driven GitHub settings.
type config struct {
	URL           string        `env:"GITHUB_URL"            envDefault:"https://api.github.com"`
	Token         string        `env:"GITHUB_TOKEN"`
	Org           string        `env:"GITHUB_ORG"`
	HTTPTimeout   time.Duration `env:"GITHUB_HTTP_TIMEOUT"   envDefault:"30s"`
	PageSize      int           `env:"GITHUB_PAGE_SIZE"      envDefault:"100"`
	WebhookSecret string        `env:"GITHUB_WEBHOOK_SECRET"`
	WebhookPath   string        `env:"GITHUB_WEBHOOK_PATH"   envDefault:"/webhook/github"`
}

// loadConfigFromEnv parses configuration from environment variables and
// validates the result.
func loadConfigFromEnv() (*config, error) {
	cfg, err := env.ParseAs[config]()
	if err != nil {
		return nil, err
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// validate checks that all required fields are present and that optional
// fields are within acceptable bounds.
func (c config) validate() error {
	if len(c.Token) == 0 {
		return fmt.Errorf("%w: %s", ErrMissingEnvVariable, "GITHUB_TOKEN")
	}
	if len(c.Org) == 0 {
		return fmt.Errorf("%w: %s", ErrMissingEnvVariable, "GITHUB_ORG")
	}
	if c.PageSize < 1 || c.PageSize > githubMaxPageSize {
		return fmt.Errorf("%w: GITHUB_PAGE_SIZE must be between 1 and %d, got %d", ErrInvalidEnvVariable, githubMaxPageSize, c.PageSize)
	}
	return nil
}
