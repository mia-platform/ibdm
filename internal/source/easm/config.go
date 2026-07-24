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
	// Customer scopes the request to a single customer via the X-Customer
	// header. Always required: it selects whose scan results to read.
	Customer string `env:"EASM_CUSTOMER"`
	// Token authenticates the caller to the backend via Authorization: Bearer.
	// Optional for now — the backend has no auth yet; set it once auth lands.
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
// is always required: it scopes the request to a single customer via the
// X-Customer header. Token is optional for now (the backend has no auth yet)
// and, once set, authenticates the caller via Authorization: Bearer.
func validateConfig(cfg config) error {
	missing := make([]string, 0)

	if cfg.BaseURL == "" {
		missing = append(missing, "EASM_BASE_URL")
	}
	if cfg.Customer == "" {
		missing = append(missing, "EASM_CUSTOMER")
	}

	if len(missing) > 0 {
		return fmt.Errorf("%w: %s", ErrMissingEnvVariable, strings.Join(missing, ", "))
	}

	return nil
}
