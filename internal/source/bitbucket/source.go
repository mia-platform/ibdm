// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package bitbucket

import (
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/mia-platform/ibdm/internal/source"
)

var (
	// ErrBitbucketSource wraps all errors originating from the Bitbucket implementation.
	ErrBitbucketSource = errors.New("bitbucket source")
	// ErrMissingEnvVariable reports missing mandatory environment variables.
	ErrMissingEnvVariable = errors.New("missing environment variable")
	// ErrInvalidEnvVariable reports malformed environment variable values.
	ErrInvalidEnvVariable = errors.New("invalid environment value")
)

var _ source.SyncableSource = &Source{}
var _ source.WebhookSource = &Source{}

// Source implements source.SyncableSource and source.WebhookSource for Bitbucket.
type Source struct {
	workspace     string
	webhookConfig webhookConfig
	client        *client

	syncLock sync.Mutex
}

// NewSource constructs a Source by reading its configuration from environment
// variables and initialising the underlying HTTP client. It returns
// ErrBitbucketSource if the configuration is invalid.
func NewSource() (*Source, error) {
	srcCfg, err := loadSourceConfigFromEnv()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBitbucketSource, err)
	}

	whCfg, err := loadWebhookConfigFromEnv()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBitbucketSource, err)
	}

	return &Source{
		workspace:     srcCfg.Workspace,
		webhookConfig: whCfg,
		client: &client{
			baseURL:     srcCfg.URL,
			accessToken: srcCfg.AccessToken,
			apiUsername: srcCfg.APIUsername,
			apiToken:    srcCfg.APIToken,
			httpClient: &http.Client{
				Timeout: srcCfg.HTTPTimeout,
			},
		},
	}, nil
}
