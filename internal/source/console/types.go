// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package console

// config holds the environment-driven Console settings.
type config struct {
	WebhookPath string `env:"CONSOLE_WEBHOOK_PATH" envDefault:"/console-webhook"`
}

// Source wires Console clients to satisfy source interfaces.
type consoleClient struct {
	config config
}

type Source struct {
	c *consoleClient
}
