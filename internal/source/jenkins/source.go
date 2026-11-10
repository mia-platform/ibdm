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

// handleErr wraps non-nil errors with ErrJenkinsSource. A context cancellation is a normal
// shutdown and returns nil. A deadline error is returned: the HTTP client reports its own timeout
// as one, and a slow controller is a failure. A joined error is returned when any of its members
// is a real failure, so a cancellation never masks the other failures.
func handleErr(err error) error {
	if err == nil || isCanceled(err) {
		return nil
	}

	return fmt.Errorf("%w: %w", ErrJenkinsSource, err)
}

// isCanceled reports whether err is a context cancellation and nothing else: an error wrapping
// several errors is a cancellation only when every wrapped error is one.
func isCanceled(err error) bool {
	var joined interface{ Unwrap() []error }
	if errors.As(err, &joined) {
		members := joined.Unwrap()
		for _, member := range members {
			if !isCanceled(member) {
				return false
			}
		}
		return len(members) > 0
	}

	return errors.Is(err, context.Canceled)
}
