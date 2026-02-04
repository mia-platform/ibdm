// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package fake

import (
	"context"
	"net/http"
	"testing"

	"github.com/mia-platform/ibdm/internal/server"
)

var _ server.Server = &Server{}

type Route struct {
	Method  string
	Path    string
	Handler func(context.Context, http.Header, []byte) error
}

type Server struct {
	tb               testing.TB
	RegisteredRoutes []Route

	startedChan chan struct{}
	closedChan  chan struct{}
}

func NewFakeServer(tb testing.TB) *Server {
	tb.Helper()

	return &Server{
		tb:          tb,
		startedChan: make(chan struct{}),
		closedChan:  make(chan struct{}),
	}
}

func (s *Server) AddRoute(method string, path string, handler func(ctx context.Context, headers http.Header, body []byte) error) {
	s.tb.Helper()
	s.RegisteredRoutes = append(s.RegisteredRoutes, Route{
		Method:  method,
		Path:    path,
		Handler: handler,
	})
}

func (s *Server) Start() error {
	s.tb.Helper()
	close(s.startedChan)
	<-s.closedChan
	return nil
}

func (s *Server) Stop() error {
	s.tb.Helper()
	close(s.closedChan)
	return nil
}

func (s *Server) StartAsync(_ context.Context) {
	s.tb.Helper()
	<-s.closedChan
}

func (s *Server) StartedServer() <-chan struct{} {
	s.tb.Helper()
	return s.startedChan
}
