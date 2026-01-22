// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package server

import (
	"context"
	"errors"
	"fmt"

	"github.com/gofiber/fiber/v2"

	"github.com/mia-platform/ibdm/internal/info"
	"github.com/mia-platform/ibdm/internal/logger"
)

const (
	serviceName = "ibdm"
	loggerName  = "ibdm:server"
)

type Server struct {
	app *fiber.App
	cfg Config
}

var (
	ErrFiberListen   = errors.New("fiber server listen error")
	ErrFiberShutdown = errors.New("fiber server shutdown error")
)

func NewServer(ctx context.Context) (*Server, error) {
	cfg, err := LoadServerConfig()
	if err != nil {
		return nil, err
	}

	app := fiber.New(fiber.Config{
		DisableStartupMessage: cfg.DisableStartupMessage,
	})
	log := logger.FromContext(ctx)
	app.Use(logger.RequestMiddlewareLogger(ctx, log, []string{"/-/"}))

	statusRoutes(app, serviceName, info.Version)

	return &Server{
		app: app,
		cfg: *cfg,
	}, nil
}

func (s Server) App() *fiber.App {
	return s.app
}

func (s *Server) Start() error {
	if err := s.app.Listen(":" + s.cfg.HTTPPort); err != nil {
		return fmt.Errorf("%w: %s", ErrFiberListen, err.Error())
	}
	return nil
}

func (s *Server) StartAsync(ctx context.Context) {
	log := logger.FromContext(ctx).WithName(loggerName)
	go func() {
		if err := s.Start(); err != nil {
			log.Error(err.Error())
		}
	}()
}

func FiberHandlerWrapper(handler func([]byte) error) fiber.Handler {
	return func(ctx *fiber.Ctx) error {
		if err := handler(ctx.Body()); err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, "handler error: "+err.Error())
		}
		return ctx.SendStatus(fiber.StatusNoContent)
	}
}
