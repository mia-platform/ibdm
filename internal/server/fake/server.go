// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package fake

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/server"
)

var _ server.Server = &Server{}

type Route struct {
	Method  string
	Path    string
	Handler func(context.Context, http.Header, []byte) error
}

type Server struct {
	tb                testing.TB
	expectedMethod    string
	expectedPath      string
	handler           func(context.Context, http.Header, []byte) error
	alreadyRegistered bool

	startedChan chan struct{}
	closedChan  chan struct{}

	once sync.Once
}

func NewFakeServer(tb testing.TB, expectedMethod, expectedPath string) *Server {
	tb.Helper()

	return &Server{
		tb:             tb,
		expectedMethod: expectedMethod,
		expectedPath:   expectedPath,
		startedChan:    make(chan struct{}),
		closedChan:     make(chan struct{}),
	}
}

func (s *Server) AddRoute(method string, path string, handler func(ctx context.Context, headers http.Header, body []byte) error) {
	s.tb.Helper()
	require.False(s.tb, s.alreadyRegistered)
	assert.Equal(s.tb, s.expectedMethod, method)
	assert.Equal(s.tb, s.expectedPath, path)
	s.handler = handler

	s.once.Do(func() {
		s.alreadyRegistered = true
	})
}

func (s *Server) Start() error {
	s.tb.Helper()
	close(s.startedChan)
	<-s.closedChan
	return nil
}

func (s *Server) Stop(_ context.Context) error {
	s.tb.Helper()
	close(s.closedChan)
	return nil
}

func (s *Server) CallRegisterWebhook(ctx context.Context) error {
	s.tb.Helper()
	require.True(s.tb, s.alreadyRegistered)

	return s.handler(ctx, nil, nil)
}

func (s *Server) StartAsync() <-chan error {
	s.tb.Helper()
	errorChan := make(chan error)
	go func() {
		<-s.closedChan
		errorChan <- nil
		close(errorChan)
	}()

	close(s.startedChan)
	return errorChan
}

func (s *Server) StartedServer() <-chan struct{} {
	s.tb.Helper()
	return s.startedChan
}

func (s *Server) StoppedServer() <-chan struct{} {
	s.tb.Helper()
	return s.closedChan
}
