// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"

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
	ErrServerListen   = errors.New("server listen error")
	ErrServerShutdown = errors.New("server shutdown error")
)

func NewServer(ctx context.Context) (*Server, error) {
	cfg, err := LoadServerConfig()
	if err != nil {
		return nil, err
	}

	app := fiber.New(fiber.Config{
		DisableStartupMessage: cfg.DisableStartupMessage,
		Immutable:             true, // ensure that accessing request body returns a copy that is valid after the request lifecycle (accessing body and headers in goroutines in the request handlers)
	})
	log := logger.FromContext(ctx)
	app.Use(logger.RequestMiddlewareLogger(ctx, log, []string{"/-/"}))

	statusRoutes(app, serviceName, info.Version)

	return &Server{
		app: app,
		cfg: *cfg,
	}, nil
}

func (s *Server) AddRoute(method string, path string, handler func(ctx context.Context, headers http.Header, body []byte) error) {
	s.app.Add(method, path, func(ctx *fiber.Ctx) error {
		if err := handler(ctx.UserContext(), ctx.GetReqHeaders(), ctx.Body()); err != nil {
			return ctx.Status(http.StatusInternalServerError).JSON(fiber.Map{
				"statusCode": http.StatusInternalServerError,
				"error":      http.StatusText(http.StatusInternalServerError),
				"message":    "error processing webhook message",
			})
		}
		return ctx.SendStatus(http.StatusNoContent)
	})
}

func (s *Server) Start() error {
	if err := s.app.Listen(fmt.Sprintf("%s:%d", s.cfg.HTTPHost, s.cfg.HTTPPort)); err != nil {
		return fmt.Errorf("%w: %w", ErrServerListen, err)
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
