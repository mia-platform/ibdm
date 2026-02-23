// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package gitlab

import (
	"net/http"
	"sync"

	"github.com/mia-platform/ibdm/internal/source"
)

// sourceConfig holds the environment-driven GitLab API settings.
type sourceConfig struct {
	Token   string `env:"GITLAB_TOKEN,required"`
	BaseURL string `env:"GITLAB_BASE_URL" envDefault:"https://gitlab.com"`
}

// webhookConfig holds the environment-driven GitLab webhook settings.
type webhookConfig struct {
	WebhookPath  string `env:"GITLAB_WEBHOOK_PATH" envDefault:"/gitlab/webhook"`
	WebhookToken string `env:"GITLAB_WEBHOOK_TOKEN"`
}

// gitLabClient wraps the HTTP client and source configuration for GitLab API requests.
type gitLabClient struct {
	config sourceConfig
	http   *http.Client
}

// Source implements [source.WebhookSource] and [source.SyncableSource] for GitLab.
// It can both poll resources via the GitLab REST API and receive real-time pipeline
// events through a token-authenticated webhook.
type Source struct {
	c             *gitLabClient
	webhookConfig webhookConfig
	syncLock      sync.Mutex
}

var _ source.WebhookSource = &Source{}
var _ source.SyncableSource = &Source{}
