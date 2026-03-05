// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package nexus

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

var (
	// ErrMissingEnvVariable reports missing mandatory environment variables.
	ErrMissingEnvVariable = errors.New("missing environment variable")
	// ErrInvalidEnvVariable reports malformed environment variable values.
	ErrInvalidEnvVariable = errors.New("invalid environment value")
)

// config holds the environment-driven Nexus settings.
type config struct {
	URLSchema          string        `env:"NEXUS_URL_SCHEMA"`
	URLHost            string        `env:"NEXUS_URL_HOST"`
	TokenName          string        `env:"NEXUS_TOKEN_NAME"`
	TokenPasscode      string        `env:"NEXUS_TOKEN_PASSCODE"`
	HTTPTimeout        time.Duration `env:"NEXUS_HTTP_TIMEOUT"        envDefault:"30s"`
	SpecificRepository string        `env:"NEXUS_SPECIFIC_REPOSITORY"`
}

// loadConfigFromEnv parses environment variables into a config struct and
// validates that all required fields are present.
func loadConfigFromEnv() (config, error) {
	cfg, err := env.ParseAs[config]()
	if err != nil {
		return config{}, err
	}

	if err := validateConfig(cfg); err != nil {
		return config{}, err
	}

	return cfg, nil
}

// validateConfig checks that the required config fields are non-empty.
func validateConfig(cfg config) error {
	missing := make([]string, 0)

	if cfg.URLSchema == "" {
		missing = append(missing, "NEXUS_URL_SCHEMA")
	}
	if cfg.URLHost == "" {
		missing = append(missing, "NEXUS_URL_HOST")
	}
	if cfg.TokenName == "" {
		missing = append(missing, "NEXUS_TOKEN_NAME")
	}
	if cfg.TokenPasscode == "" {
		missing = append(missing, "NEXUS_TOKEN_PASSCODE")
	}

	if len(missing) > 0 {
		return fmt.Errorf("%w: %s", ErrMissingEnvVariable, strings.Join(missing, ", "))
	}

	return nil
}
