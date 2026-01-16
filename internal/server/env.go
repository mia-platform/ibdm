// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package server

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/caarlos0/env/v11"
)

var (
	ErrEnvVariablesNotValid = errors.New("environment variables not valid")
)

type Config struct {
	LoggerLevel           string `env:"LOGGER_LEVEL" envDefault:"Info"`
	DisableStartupMessage bool   `env:"DISABLE_STARTUP_MESSAGE" envDefault:"true"`
	HTTPPort              string `env:"HTTP_PORT" envDefault:"3000"`
}

func LoadServerConfig() (*Config, error) {
	var envVars Config
	if err := env.Parse(&envVars); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrEnvVariablesNotValid, err.Error())
	}

	if err := validateEnvironmentVariables(&envVars); err != nil {
		return nil, err
	}
	return &envVars, nil
}

func validateEnvironmentVariables(envVars *Config) error {
	envError := make([]string, 0)

	serverPortNumber, err := strconv.Atoi(envVars.HTTPPort)
	if err != nil {
		envError = append(envError, "HTTP_PORT is not a valid number")
	}
	if serverPortNumber < 1 || serverPortNumber > 65535 {
		envError = append(envError, "HTTP_PORT is out of valid range (1-65535)")
	}

	if len(envError) > 0 {
		return fmt.Errorf("%w: %s", ErrEnvVariablesNotValid, strings.Join(envError, ", "))
	}
	return nil
}
