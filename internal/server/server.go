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
)

type Server interface {
	AddRoute(method string, path string, handler func(ctx context.Context, headers http.Header, body []byte) error)
	Start() error
	Stop(context.Context) error
	StartAsync() <-chan error
}

type impServer struct {
	config

	app *fiber.App
}

var (
	ErrServerListen   = errors.New("server listen error")
	ErrServerShutdown = errors.New("server shutdown error")
)

func NewServer(ctx context.Context) (Server, error) {
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

	return &impServer{
		app:    app,
		config: *cfg,
	}, nil
}

func (s *impServer) AddRoute(method string, path string, handler func(ctx context.Context, headers http.Header, body []byte) error) {
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

func (s *impServer) Start() error {
	if err := s.app.Listen(fmt.Sprintf("%s:%d", s.HTTPHost, s.HTTPPort)); err != nil {
		return fmt.Errorf("%w: %w", ErrServerListen, err)
	}
	return nil
}

func (s *impServer) Stop(ctx context.Context) error {
	if err := s.app.ShutdownWithContext(ctx); err != nil {
		return fmt.Errorf("%w: %w", ErrServerShutdown, err)
	}
	return nil
}

func (s *impServer) StartAsync() <-chan error {
	errorChan := make(chan error)
	go func() {
		err := s.Start()
		errorChan <- err
		close(errorChan)
	}()

	return errorChan
}
