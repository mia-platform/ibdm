// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package azure

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/caarlos0/env/v11"

	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/mia-platform/ibdm/internal/source"
)

var (
	// ErrAzureSource is the sentinel error for all Azure Source errors.
	ErrAzureSource = errors.New("azure source")
)

const (
	logName = "ibdm:source:azure"
)

var _ source.SyncableSource = &Source{}
var _ source.EventSource = &Source{}
var _ source.ClosableSource = &Source{}

// Source implement both source.StreamableSource and source.SyncableSource for Azure.
type Source struct {
	config

	syncLock    sync.Mutex
	syncContext atomic.Pointer[syncContext]
}

// syncContext holds references needed for a sync process lifecycle.
type syncContext struct {
	cancel context.CancelFunc
}

// NewSource creates a new Azure Source reading the needed configuration from the env variables.
func NewSource() (*Source, error) {
	config, err := env.ParseAs[config]()
	if err != nil {
		return nil, handleError(err)
	}

	return &Source{
		config: config,
	}, nil
}

// StartSyncProcess implement source.SyncableSource.
func (s *Source) StartSyncProcess(ctx context.Context, _ []string, _ chan<- source.Data) error {
	log := logger.FromContext(ctx).WithName(logName)
	if !s.syncLock.TryLock() {
		log.Debug("sync process already running")
		return nil
	}
	defer s.syncLock.Unlock()

	if err := s.validateForSync(); err != nil {
		return handleError(err)
	}

	return nil
}

// StartEventStream implement source.EventSource.
func (s *Source) StartEventStream(context.Context, []string, chan<- source.Data) error {
	if err := s.validateForEventStream(); err != nil {
		return handleError(err)
	}

	return nil
}

func (s *Source) Close(ctx context.Context, _ time.Duration) error {
	log := logger.FromContext(ctx).WithName(logName)
	log.Debug("closing Microsoft Azure client")

	syncClient := s.syncContext.Swap(nil)
	if syncClient != nil {
		syncClient.cancel()
	}

	log.Trace("closed Microsoft Azure client")
	return nil
}

// handleError always wraps the given error with ErrAzureSource.
// It also unwraps some errors to cleanup the error message and removing unnecessary layers.
func handleError(err error) error {
	if err == nil {
		return nil
	}

	switch u := errors.Unwrap(err); u != nil {
	case errors.Is(u, env.ParseError{}):
	default:
		err = u
	}

	return fmt.Errorf("%w: %w", ErrAzureSource, err)
}
