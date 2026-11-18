// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package server

import (
	"errors"
	"fmt"
	"strings"

	"github.com/caarlos0/env/v11"
)

var (
	ErrEnvVariablesNotValid = errors.New("environment variables not valid")
)

type config struct {
	DisableStartupMessage bool   `env:"DISABLE_STARTUP_MESSAGE" envDefault:"true"`
	HTTPHost              string `env:"HTTP_HOST" envDefault:"0.0.0.0"`
	HTTPPort              int    `env:"HTTP_PORT" envDefault:"3000"`
	// MaxBodySize is the largest request body accepted, in bytes; larger bodies are answered 413.
	MaxBodySize int64 `env:"HTTP_MAX_BODY_SIZE" envDefault:"4194304"`
}

func LoadServerConfig() (*config, error) {
	var envVars config
	if err := env.Parse(&envVars); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrEnvVariablesNotValid, err.Error())
	}

	if err := validateEnvironmentVariables(&envVars); err != nil {
		return nil, err
	}
	return &envVars, nil
}

func validateEnvironmentVariables(envVars *config) error {
	envError := make([]string, 0)

	if envVars.HTTPPort < 0 || envVars.HTTPPort > 65535 {
		envError = append(envError, "HTTP_PORT is out of valid range (1-65535)")
	}

	if envVars.MaxBodySize <= 0 {
		envError = append(envError, "HTTP_MAX_BODY_SIZE must be a positive number of bytes")
	}

	if len(envError) > 0 {
		return fmt.Errorf("%w: %s", ErrEnvVariablesNotValid, strings.Join(envError, ", "))
	}
	return nil
}
