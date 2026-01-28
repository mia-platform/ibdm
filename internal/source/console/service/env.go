// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package service

import (
	"errors"
	"fmt"
	"net/url"

	"github.com/caarlos0/env/v11"
)

var (
	errParsingConfig       = errors.New("error parsing console configuration from environment variables")
	errMissingClientID     = errors.New("CONSOLE_CLIENT_ID is required when CONSOLE_CLIENT_SECRET is set")
	errMissingClientSecret = errors.New("CONSOLE_CLIENT_SECRET is required when CONSOLE_CLIENT_ID is set")
)

// config holds the environment-driven Console settings.
type config struct {
	ConsoleEndpoint string `env:"CONSOLE_ENDPOINT,required"`
	ClientID        string `env:"CONSOLE_CLIENT_ID"`
	ClientSecret    string `env:"CONSOLE_CLIENT_SECRET"`
	AuthEndpoint    string `env:"CONSOLE_AUTH_ENDPOINT"`
}

func loadConfigFromEnv() (*config, error) {
	config, err := env.ParseAs[config]()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", errParsingConfig, err.Error())
	}
	if err := config.validate(); err != nil {
		return nil, err
	}
	return &config, nil
}

func (c *config) validate() error {
	if c.ConsoleEndpoint == "" {
		return errors.New("CONSOLE_ENDPOINT is required")
	}
	endpointURL, err := url.Parse(c.ConsoleEndpoint)
	if err != nil {
		return fmt.Errorf("invalid CONSOLE_ENDPOINT: %w", err)
	}

	switch {
	case len(c.ClientID) > 0 && len(c.ClientSecret) == 0:
		return errMissingClientSecret
	case len(c.ClientSecret) > 0 && len(c.ClientID) == 0:
		return errMissingClientID
	}

	if len(c.AuthEndpoint) == 0 {
		endpointURL.Path = "/oauth/token"
		c.AuthEndpoint = endpointURL.String()
	} else {
		_, err := url.Parse(c.AuthEndpoint)
		if err != nil {
			return fmt.Errorf("invalid CONSOLE_AUTH_ENDPOINT: %w", err)
		}
	}
	return nil
}
