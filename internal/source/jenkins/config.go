// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package jenkins

import (
	"errors"
	"fmt"
	"net/url"
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

const (
	urlEnvName      = "JENKINS_URL"
	usernameEnvName = "JENKINS_USERNAME"
	apiTokenEnvName = "JENKINS_API_TOKEN"
	timeoutEnvName  = "JENKINS_HTTP_TIMEOUT"
)

// config holds the environment-driven Jenkins settings.
type config struct {
	URL         string        `env:"JENKINS_URL"`
	Username    string        `env:"JENKINS_USERNAME"`
	APIToken    string        `env:"JENKINS_API_TOKEN"`
	HTTPTimeout time.Duration `env:"JENKINS_HTTP_TIMEOUT" envDefault:"30s"`
}

// loadConfigFromEnv parses the JENKINS_* environment variables into a config, validates it and
// normalises the controller URL by removing any trailing slash.
func loadConfigFromEnv() (config, error) {
	cfg, err := env.ParseAs[config]()
	if err != nil {
		return config{}, fmt.Errorf("%w: %w", ErrInvalidEnvVariable, err)
	}

	if err := cfg.validate(); err != nil {
		return config{}, err
	}

	cfg.URL = strings.TrimRight(cfg.URL, "/")
	return cfg, nil
}

// validate checks that the required fields are set and that the controller URL and the HTTP
// timeout are usable.
func (c config) validate() error {
	missing := make([]string, 0)
	if c.URL == "" {
		missing = append(missing, urlEnvName)
	}
	if c.Username == "" {
		missing = append(missing, usernameEnvName)
	}
	if c.APIToken == "" {
		missing = append(missing, apiTokenEnvName)
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w: %s", ErrMissingEnvVariable, strings.Join(missing, ", "))
	}

	if err := validateURL(c.URL); err != nil {
		return err
	}

	if c.HTTPTimeout <= 0 {
		return fmt.Errorf("%w: %s must be a positive duration", ErrInvalidEnvVariable, timeoutEnvName)
	}

	return nil
}

// validateURL accepts only absolute http or https URLs with a host, without user information,
// query or fragment: the controller URL is the prefix of every API request.
func validateURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("%w: %s is not a valid URL", ErrInvalidEnvVariable, urlEnvName)
	}

	switch {
	case parsed.Scheme != "http" && parsed.Scheme != "https":
		return fmt.Errorf("%w: %s must use the http or https scheme", ErrInvalidEnvVariable, urlEnvName)
	case parsed.Host == "":
		return fmt.Errorf("%w: %s must include a host", ErrInvalidEnvVariable, urlEnvName)
	case parsed.User != nil:
		return fmt.Errorf("%w: %s must not include user information", ErrInvalidEnvVariable, urlEnvName)
	case parsed.RawQuery != "" || parsed.Fragment != "":
		return fmt.Errorf("%w: %s must not include a query or a fragment", ErrInvalidEnvVariable, urlEnvName)
	}

	return nil
}
