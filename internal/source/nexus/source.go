// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package nexus

import (
	"errors"
	"sync"
	"time"

	"github.com/mia-platform/ibdm/internal/source"
)

const (
	loggerName = "ibdm:source:nexus"

	dockerImageType = "dockerimage"
)

var (
	// ErrNexusSource wraps errors emitted by the Nexus source implementation.
	ErrNexusSource = errors.New("nexus source")

	// timeSource is a replaceable function for obtaining the current time.
	// Tests override this to produce deterministic timestamps.
	timeSource = time.Now
)

var _ source.SyncableSource = &Source{}
var _ source.WebhookSource = &Source{}

// Source implements source.SyncableSource and source.WebhookSource for Nexus Repository Manager.
type Source struct {
	config        config
	webhookConfig webhookConfig
	client        *client

	syncLock sync.Mutex
}

// NewSource creates a new Nexus Source reading configuration from environment variables.
func NewSource() (*Source, error) {
	cfg, err := loadConfigFromEnv()
	if err != nil {
		return nil, handleErr(err)
	}

	whCfg, err := loadWebhookConfigFromEnv()
	if err != nil {
		return nil, handleErr(err)
	}

	c, err := newClient(cfg)
	if err != nil {
		return nil, handleErr(err)
	}

	return &Source{
		config:        cfg,
		webhookConfig: whCfg,
		client:        c,
	}, nil
}
