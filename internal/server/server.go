// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package server

import (
	"context"
	"errors"
	"fmt"

	"github.com/gofiber/fiber/v2"

	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/mia-platform/ibdm/internal/version"
)

const serviceName = "ibdm"

var (
	ErrFiberServerShutdown    = errors.New("fiber server shutdown error")
	ErrFiberServerEnvNotValid = errors.New("fiber server environment variables not valid")
)

func NewServer(ctx context.Context, envs EnvironmentVariables) (*fiber.App, func() error) {
	app := fiber.New(fiber.Config{
		DisableStartupMessage: envs.DisableStartupMessage,
	})
	log := logger.FromContext(ctx)
	app.Use(logger.RequestMiddlewareLogger(ctx, log, []string{"/-/"}))

	statusRoutes(app, serviceName, version.ServiceVersionInformation())

	return app, func() error {
		log.Info("shutting down server")
		err := app.Shutdown()
		if err != nil {
			return fmt.Errorf("%w: %s", ErrFiberServerShutdown, err.Error())
		}
		log.Info("server shutdown complete")
		return nil
	}
}
