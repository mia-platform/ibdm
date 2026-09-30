// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package sonarqube

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"

	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/mia-platform/ibdm/internal/source"
)

const (
	// sonarSignatureHeader carries hex(hmac_sha256(secret, raw body)), bare
	// hex without the sha256= prefix of GitHub-style webhooks.
	sonarSignatureHeader = "X-Sonar-Webhook-HMAC-SHA256"
)

// GetWebhook implements [source.WebhookSource]. It returns a [source.Webhook]
// that verifies the SonarQube HMAC signature, answers immediately and reads
// the analysis issues in the background: SonarQube times a webhook out after
// ten seconds, and reading a large project takes longer. Every delivery is a
// run; a successful one also produces the issues of the analysed ref.
//
// A secret is required unless SONARQUBE_WEBHOOK_ALLOW_UNSIGNED is set: the
// route carries no other authentication, since SonarQube holds no token the
// platform issues. With a secret configured every delivery is verified,
// whatever the value of SONARQUBE_WEBHOOK_ALLOW_UNSIGNED.
func (s *Source) GetWebhook(_ context.Context, typesToStream map[string]source.Extra, results chan<- source.Data) (source.Webhook, error) {
	if s.webhookConfig.WebhookSecret == "" && !s.webhookConfig.AllowUnsigned {
		return source.Webhook{}, fmt.Errorf("%w: %w: %s (or set SONARQUBE_WEBHOOK_ALLOW_UNSIGNED=true to accept unauthenticated deliveries)",
			ErrSonarQubeSource, ErrMissingEnvVariable, "SONARQUBE_WEBHOOK_SECRET")
	}

	return source.Webhook{
		Method: http.MethodPost,
		Path:   s.webhookConfig.WebhookPath,
		Handler: func(ctx context.Context, headers http.Header, body []byte) error {
			log := logger.FromContext(ctx).WithName(loggerName)

			if secret := s.webhookConfig.WebhookSecret; secret != "" {
				if !verifySignature(body, headers.Get(sonarSignatureHeader), secret) {
					err := fmt.Errorf("%w: missing or invalid %s header", ErrSonarQubeSource, sonarSignatureHeader)
					log.Error("webhook request with missing or invalid signature", "error", err.Error())
					return err
				}
			}

			// Parsed before answering: the body buffer is not guaranteed to
			// outlive the request, and a body that is not a payload is refused.
			payload, err := parseWebhookPayload(body)
			if err != nil {
				log.Error("failed to parse webhook payload", "error", err.Error())
				return fmt.Errorf("%w: %w", ErrSonarQubeSource, err)
			}

			if !mapsAny(typesToStream) {
				log.Debug("neither issue nor run is mapped, ignoring delivery", "project", payload.Project.Key)
				return nil
			}
			// A task that did not succeed never completed its analysis: reading
			// the project would record the previous analysis' findings as if they
			// were this one's. It is still a run, and recorded as one.
			if _, runsMapped := typesToStream[runType]; !payload.isSuccessfulAnalysis() && !runsMapped {
				log.Debug("ignoring an analysis that did not succeed", "project", payload.Project.Key, "status", payload.Status)
				return nil
			}
			if payload.ServerURL != "" && strings.TrimRight(payload.ServerURL, "/") != s.config.URL {
				log.Debug("the delivery names another server URL; reading from SONARQUBE_URL", "serverUrl", payload.ServerURL, "project", payload.Project.Key)
			}

			go func(ctx context.Context) {
				if err := s.processAnalysis(ctx, payload, typesToStream, results); err != nil {
					log.Error("error processing webhook delivery", "project", payload.Project.Key, "error", err.Error())
				}
			}(ctx)

			return nil
		},
	}, nil
}

// processAnalysis emits the run of the analysis a delivery announces and, when
// it succeeded, the issues it left on the analysed ref.
func (s *Source) processAnalysis(ctx context.Context, payload *webhookPayload, types map[string]source.Extra, results chan<- source.Data) error {
	scope := payload.scope()

	// Only a branch is a git ref: a pull request analysis reports its PR key
	// as the branch name, which names nothing in the repository.
	scm := s.resolveSCMTarget(ctx, payload.Project.Key, payload.property(repositoryURLProperty), payload.Revision, scope.branch())

	analysis := analysisContext{
		serverURL:   s.publicURL(payload.ServerURL),
		projectKey:  payload.Project.Key,
		projectName: payload.Project.Name,
		branch:      scope.branch(),
		pullRequest: scope.pullRequest(),
		taskID:      payload.TaskID,
		analysedAt:  rfc3339(payload.AnalysedAt),
		revision:    payload.Revision,
		scm:         scm,
	}
	if payload.Branch != nil {
		analysis.isMainBranch = payload.Branch.IsMain
	}
	run := runInfo{status: payload.Status}
	if payload.QualityGate != nil {
		analysis.qualityGateStatus = payload.QualityGate.Status
		analysis.qualityGateName = payload.QualityGate.Name
		run.conditions = payload.QualityGate.Conditions
	}

	eventTime, ok := parseTime(payload.AnalysedAt)
	if !ok {
		eventTime = timeSource()
	}

	return handleErr(s.emitAnalysis(ctx, analysis, scope, run, payload.isSuccessfulAnalysis(), types, eventTime, results))
}

// verifySignature checks the HMAC-SHA256 signature SonarQube computes over the
// raw body. It compares in constant time; a missing, empty or non-hex header
// is a plain mismatch.
func verifySignature(body []byte, signature, secret string) bool {
	signature = strings.TrimSpace(signature)
	if signature == "" {
		return false
	}

	provided, err := hex.DecodeString(signature)
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)

	return hmac.Equal(provided, mac.Sum(nil))
}
