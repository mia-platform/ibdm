// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package jenkins

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/mia-platform/ibdm/internal/source"
)

const loggerName = "ibdm:source:jenkins"

var (
	// ErrJenkinsSource wraps all errors originating from the Jenkins source.
	ErrJenkinsSource = errors.New("jenkins source")

	// timeSource is a replaceable function for obtaining the current time.
	// Tests override this to produce deterministic timestamps.
	timeSource = time.Now
)

var _ source.SyncableSource = &Source{}

// Source implements source.SyncableSource for a self-hosted Jenkins controller.
type Source struct {
	client *client

	syncLock sync.Mutex
}

// NewSource constructs a Source by reading its configuration from environment variables and
// initialising the underlying HTTP client. It returns ErrJenkinsSource if the configuration is
// missing or invalid.
func NewSource() (*Source, error) {
	cfg, err := loadConfigFromEnv()
	if err != nil {
		return nil, handleErr(err)
	}

	return &Source{
		client: &client{
			baseURL:  cfg.URL,
			username: cfg.Username,
			apiToken: cfg.APIToken,
			httpClient: &http.Client{
				Timeout: cfg.HTTPTimeout,
			},
		},
	}, nil
}

// handleErr wraps non-nil errors with ErrJenkinsSource.
// Context cancellation and deadline errors are treated as a normal shutdown and return nil.
func handleErr(err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil
	}

	return fmt.Errorf("%w: %w", ErrJenkinsSource, err)
}
