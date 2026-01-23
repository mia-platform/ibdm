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
	errMissingJWTFields    = errors.New("CONSOLE_PRIVATE_KEY and CONSOLE_PRIVATE_KEY_ID are required when CONSOLE_JWT_SERVICE_ACCOUNT is true")
)

// config holds the environment-driven Console settings.
type config struct {
	ConsoleEndpoint          string `env:"CONSOLE_ENDPOINT,required"`
	ClientID                 string `env:"CONSOLE_CLIENT_ID"`
	ClientSecret             string `env:"CONSOLE_CLIENT_SECRET"`
	AuthEndpoint             string `env:"CONSOLE_AUTH_ENDPOINT"`
	PrivateKey               string `env:"CONSOLE_PRIVATE_KEY"`
	PrivateKeyID             string `env:"CONSOLE_PRIVATE_KEY_ID"`
	ConsoleJWTServiceAccount bool   `env:"CONSOLE_JWT_SERVICE_ACCOUNT" envDefault:"false"`
}

func loadConfigFromEnv() (*config, error) {
	config, err := env.ParseAs[config]()
	if err != nil {
		return nil, handleError(fmt.Errorf("%w: %s", errParsingConfig, err.Error()))
	}
	if err := config.validate(); err != nil {
		return nil, err
	}
	return &config, nil
}

func (c *config) validate() error {
	if c.ConsoleEndpoint == "" {
		return handleError(errors.New("CONSOLE_ENDPOINT is required"))
	}
	endpointURL, err := url.Parse(c.ConsoleEndpoint)
	if err != nil {
		return handleError(fmt.Errorf("invalid CONSOLE_ENDPOINT: %w", err))
	}

	switch {
	case len(c.ClientID) > 0 && len(c.ClientSecret) == 0:
		return handleError(errMissingClientSecret)
	case len(c.ClientSecret) > 0 && len(c.ClientID) == 0:
		return handleError(errMissingClientID)
	case c.ConsoleJWTServiceAccount && (len(c.PrivateKey) == 0 || len(c.PrivateKeyID) == 0):
		return handleError(errMissingJWTFields)
	}

	if len(c.AuthEndpoint) == 0 {
		endpointURL.Path = "/oauth/token"
		c.AuthEndpoint = endpointURL.String()
	} else {
		_, err := url.Parse(c.AuthEndpoint)
		if err != nil {
			return handleError(fmt.Errorf("invalid CONSOLE_AUTH_ENDPOINT: %w", err))
		}
	}
	return nil
}
