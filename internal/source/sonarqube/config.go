// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package sonarqube

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

const (
	// sonarqubeMaxPageSize is the upper bound SonarQube enforces on the `ps`
	// parameter of /api/issues/search.
	sonarqubeMaxPageSize = 500
)

var (
	// ErrMissingEnvVariable reports missing mandatory environment variables.
	ErrMissingEnvVariable = errors.New("missing environment variable")
	// ErrInvalidEnvVariable reports malformed environment variable values.
	ErrInvalidEnvVariable = errors.New("invalid environment value")
)

// config holds the environment-driven SonarQube settings shared by sync and webhook.
type config struct {
	// URL is where every Web API call goes. The serverUrl carried by a webhook
	// payload is never followed: that would let a request body choose the
	// destination of a request carrying the SonarQube token.
	URL string `env:"SONARQUBE_URL"`
	// PublicURL is the address a person opens SonarQube at, used for the link on
	// each item. Empty means the payload serverUrl (webhook) or URL (sync).
	PublicURL     string        `env:"SONARQUBE_PUBLIC_URL"`
	Token         string        `env:"SONARQUBE_TOKEN"`
	HTTPTimeout   time.Duration `env:"SONARQUBE_HTTP_TIMEOUT"   envDefault:"30s"`
	IssueStatuses string        `env:"SONARQUBE_ISSUE_STATUSES" envDefault:"OPEN,CONFIRMED"`
	NewCodeOnly   bool          `env:"SONARQUBE_NEW_CODE_ONLY"  envDefault:"false"`
	PageSize      int           `env:"SONARQUBE_PAGE_SIZE"      envDefault:"500"`
	MaxIssues     int           `env:"SONARQUBE_MAX_ISSUES"     envDefault:"20000"`
	ProjectKeys   []string      `env:"SONARQUBE_PROJECT_KEYS"   envSeparator:","`

	SCMProvider         string `env:"SONARQUBE_SCM_PROVIDER"          envDefault:"auto"`
	SCMAnalysisProperty string `env:"SONARQUBE_SCM_ANALYSIS_PROPERTY" envDefault:"sonar.analysis.repoUrl"`
	SCMUseProjectLinks  bool   `env:"SONARQUBE_SCM_USE_PROJECT_LINKS" envDefault:"true"`
	SCMRepositoryURL    string `env:"SONARQUBE_SCM_REPOSITORY_URL"`
	SCMFileTemplate     string `env:"SONARQUBE_SCM_URL_TEMPLATE"`
	SCMLineTemplate     string `env:"SONARQUBE_SCM_LINE_TEMPLATE"`
}

// webhookConfig holds the environment-driven SonarQube webhook settings.
type webhookConfig struct {
	WebhookPath   string `env:"SONARQUBE_WEBHOOK_PATH"           envDefault:"/sonarqube/webhook"`
	WebhookSecret string `env:"SONARQUBE_WEBHOOK_SECRET"`
	AllowUnsigned bool   `env:"SONARQUBE_WEBHOOK_ALLOW_UNSIGNED" envDefault:"false"`
}

// loadConfigFromEnv parses configuration from environment variables and
// validates the result.
func loadConfigFromEnv() (config, error) {
	cfg, err := env.ParseAs[config]()
	if err != nil {
		return config{}, err
	}

	if err := cfg.validate(); err != nil {
		return config{}, err
	}

	cfg.URL = strings.TrimRight(cfg.URL, "/")
	cfg.PublicURL = strings.TrimRight(cfg.PublicURL, "/")
	cfg.ProjectKeys = cleanList(cfg.ProjectKeys)

	return cfg, nil
}

// loadWebhookConfigFromEnv parses SONARQUBE_WEBHOOK_* environment variables into a webhookConfig.
func loadWebhookConfigFromEnv() (webhookConfig, error) {
	return env.ParseAs[webhookConfig]()
}

// validate checks that all required fields are present and that optional
// fields are within acceptable bounds.
func (c config) validate() error {
	missing := make([]string, 0)
	if c.URL == "" {
		missing = append(missing, "SONARQUBE_URL")
	}
	if c.Token == "" {
		missing = append(missing, "SONARQUBE_TOKEN")
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w: %s", ErrMissingEnvVariable, strings.Join(missing, ", "))
	}

	if err := validateHTTPURL("SONARQUBE_URL", c.URL); err != nil {
		return err
	}
	if c.PublicURL != "" {
		if err := validateHTTPURL("SONARQUBE_PUBLIC_URL", c.PublicURL); err != nil {
			return err
		}
	}

	if c.PageSize < 1 || c.PageSize > sonarqubeMaxPageSize {
		return fmt.Errorf("%w: SONARQUBE_PAGE_SIZE must be between 1 and %d, got %d", ErrInvalidEnvVariable, sonarqubeMaxPageSize, c.PageSize)
	}
	if c.MaxIssues < 1 {
		return fmt.Errorf("%w: SONARQUBE_MAX_ISSUES must be at least 1, got %d", ErrInvalidEnvVariable, c.MaxIssues)
	}

	if _, err := parseProvider(c.SCMProvider); err != nil {
		return err
	}

	return nil
}

// validateHTTPURL checks that value is an absolute http(s) URL. Checked at
// startup: a typo in a base URL otherwise surfaces hours later as a connection
// error on the first webhook.
func validateHTTPURL(name, value string) error {
	parsed, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("%w: %s is not a valid URL: %w", ErrInvalidEnvVariable, name, err)
	}
	if !isHTTPURL(parsed) {
		return fmt.Errorf("%w: %s must be an absolute http or https URL, got %q", ErrInvalidEnvVariable, name, value)
	}
	return nil
}

// cleanList trims every element and drops the empty ones.
func cleanList(values []string) []string {
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}
	return cleaned
}
