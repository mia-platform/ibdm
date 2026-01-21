// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package console

import "github.com/mia-platform/ibdm/internal/source"

// config holds the environment-driven Console settings.
type config struct {
	WebhookPath string `env:"CONSOLE_WEBHOOK_PATH" envDefault:"/console-webhook"`
}

// Source wires Console clients to satisfy source interfaces.
type consoleClient struct {
	config config
}

var _ source.WebhookSource = &Source{}

type Source struct {
	c *consoleClient
}
