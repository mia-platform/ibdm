// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package easm

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

// config holds the environment-driven EASM settings.
type config struct {
	// BaseURL is the FE backend base URL, e.g. http://localhost:8000 (mock) or the product backend (prod).
	BaseURL string `env:"EASM_BASE_URL"`
	// DataPath is the path of the read endpoint appended to BaseURL.
	DataPath string `env:"EASM_DATA_PATH" envDefault:"/data"`
	// Customer scopes the request via the X-Customer header (mock / no-auth topology).
	Customer string `env:"EASM_CUSTOMER"`
	// Token scopes the request via the Authorization: Bearer header (prod topology).
	Token string `env:"EASM_TOKEN"`
	// HTTPTimeout bounds each request to the endpoint.
	HTTPTimeout time.Duration `env:"EASM_HTTP_TIMEOUT" envDefault:"30s"`
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

// validateConfig checks that the required config fields are non-empty. Customer
// scoping must travel as a credential, so at least one of Customer or Token is
// required; the endpoint resolves the customer from whichever is provided.
func validateConfig(cfg config) error {
	missing := make([]string, 0)

	if cfg.BaseURL == "" {
		missing = append(missing, "EASM_BASE_URL")
	}
	if cfg.Customer == "" && cfg.Token == "" {
		missing = append(missing, "EASM_CUSTOMER or EASM_TOKEN")
	}

	if len(missing) > 0 {
		return fmt.Errorf("%w: %s", ErrMissingEnvVariable, strings.Join(missing, ", "))
	}

	return nil
}
