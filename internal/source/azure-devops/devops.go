// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package azuredevops

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
	ErrDevOpsSource = errors.New("azure devops source")
)

const (
	logName = "ibdm:source:azuredevops"
)

var _ source.SyncableSource = &Source{}
var _ source.ClosableSource = &Source{}

// Source implement both source.WebhookSource and source.SyncableSource for Azure DevOps.
type Source struct {
	config

	syncContext atomic.Pointer[processContext]
	syncLock    sync.Mutex
}

// processContext holds references needed for a sync process lifecycle.
type processContext struct {
	cancel context.CancelFunc
}

// NewSource creates a new Azure DevOps Source reading the needed configuration from the env variables.
func NewSource() (*Source, error) {
	config, err := env.ParseAs[config]()
	if err != nil {
		return nil, handleErr(err)
	}

	return &Source{
		config: config,
	}, nil
}

// StartSyncProcess implement source.SyncableSource interface.
func (s *Source) StartSyncProcess(ctx context.Context, typesToFilter map[string]source.Extra, dataChannel chan<- source.Data) error {
	log := logger.FromContext(ctx).WithName(logName)
	if !s.syncLock.TryLock() {
		log.Debug("sync process already running")
		return nil
	}
	defer s.syncLock.Unlock()

	if err := s.validate(); err != nil {
		return handleErr(err)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	s.syncContext.Store(&processContext{
		cancel: cancel,
	})

	err := syncResources(ctx, s.connection(), typesToFilter, dataChannel)

	s.syncContext.Store(nil)
	return handleErr(err)
}

// Close implement source.ClosableSource interface.
func (s *Source) Close(ctx context.Context, _ time.Duration) error {
	log := logger.FromContext(ctx).WithName(logName)
	log.Debug("closing Microsoft Azure client")

	syncClient := s.syncContext.Swap(nil)
	if syncClient != nil {
		log.Debug("cancelling sync process")
		syncClient.cancel()
	}

	log.Trace("closed Microsoft Azure client")
	return nil
}

// handleError always wraps the given error with ErrAzureDevOpsSource.
// It also unwraps some errors to cleanup the error message and removing unnecessary layers.
func handleErr(err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, context.Canceled) {
		return nil
	}

	return fmt.Errorf("%w: %s", ErrDevOpsSource, err.Error())
}
