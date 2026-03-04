// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package sysdig

import (
	"errors"
	"fmt"
	"time"

	"github.com/caarlos0/env/v11"
)

const (
	//TODO: The value should be tuned after observing real SysQL response sizes during integration testing.

	// sysdigMaxPageSize is the upper bound for SYSDIG_PAGE_SIZE.
	sysdigMaxPageSize = 1000
)

var (
	// ErrMissingEnvVariable reports missing mandatory environment variables.
	ErrMissingEnvVariable = errors.New("missing environment variable")
	// ErrInvalidEnvVariable reports malformed environment variable values.
	ErrInvalidEnvVariable = errors.New("invalid environment value")
)

// config holds the environment-driven Sysdig settings.
type config struct {
	URL         string        `env:"SYSDIG_URL"`
	APIToken    string        `env:"SYSDIG_API_TOKEN"`
	HTTPTimeout time.Duration `env:"SYSDIG_HTTP_TIMEOUT" envDefault:"30s"`
	PageSize    int           `env:"SYSDIG_PAGE_SIZE"    envDefault:"1000"`
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
	if len(c.URL) == 0 {
		return fmt.Errorf("%w: %s", ErrMissingEnvVariable, "SYSDIG_URL")
	}
	if len(c.APIToken) == 0 {
		return fmt.Errorf("%w: %s", ErrMissingEnvVariable, "SYSDIG_API_TOKEN")
	}
	if c.PageSize < 1 || c.PageSize > sysdigMaxPageSize {
		return fmt.Errorf("%w: SYSDIG_PAGE_SIZE must be between 1 and %d, got %d", ErrInvalidEnvVariable, sysdigMaxPageSize, c.PageSize)
	}
	return nil
}
