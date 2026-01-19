// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package console

import (
	"errors"
	"fmt"
	"strings"

	"github.com/caarlos0/env/v11"
)

const (
	MessageInvalidWebhookPath = "CONSOLE_WEBHOOK_PATH must start with '/'"
)

var (
	ErrConfigNotValid = errors.New("console source configuration not valid")
)

func loadConfigFromEnv() (*config, error) {
	config, err := env.ParseAs[config]()
	if err != nil {
		return nil, err
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &config, nil
}

func (c config) Validate() error {
	errorsList := make([]string, 0)

	if !strings.HasPrefix(c.WebhookPath, "/") {
		errorsList = append(errorsList, MessageInvalidWebhookPath)
	}

	if len(errorsList) > 0 {
		return fmt.Errorf("%w: %v", ErrConfigNotValid, strings.Join(errorsList, "; "))
	}
	return nil
}
