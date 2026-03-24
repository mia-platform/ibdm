// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package github

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/mia-platform/ibdm/internal/source"
)

// afterGoroutine is a test hook invoked via defer at the end of the dispatcher
// goroutine. Production code leaves it nil.
var afterGoroutine func()

// GetWebhook implements source.WebhookSource. It validates the webhook
// configuration and returns a Webhook that verifies HMAC-SHA256 signatures
// and dispatches events to the processor registry.
func (s *Source) GetWebhook(ctx context.Context, typesToStream map[string]source.Extra, results chan<- source.Data) (source.Webhook, error) {
	if s.config.WebhookSecret == "" {
		return source.Webhook{}, fmt.Errorf("%w: %w: %s", ErrGitHubSource, ErrMissingEnvVariable, "GITHUB_WEBHOOK_SECRET")
	}

	return source.Webhook{
		Method: http.MethodPost,
		Path:   s.config.WebhookPath,
		Handler: func(ctx context.Context, headers http.Header, body []byte) error {
			log := logger.FromContext(ctx).WithName(loggerName)

			signature := headers.Get("X-Hub-Signature-256")
			if signature == "" {
				err := fmt.Errorf("%w: missing X-Hub-Signature-256 header", ErrGitHubSource)
				log.Error("webhook request missing signature header", "error", err.Error())
				return err
			}

			if !verifySignature(body, signature, s.config.WebhookSecret) {
				err := fmt.Errorf("%w: invalid webhook signature", ErrGitHubSource)
				log.Error("webhook request with invalid signature", "error", err.Error())
				return err
			}

			go func(ctx context.Context) {
				defer func() {
					if afterGoroutine != nil {
						afterGoroutine()
					}
				}()

				eventType := headers.Get(githubEventHeader)
				processor, ok := eventProcessors[eventType]
				if !ok {
					log.Debug("ignoring unsupported event", githubEventHeader, eventType)
					return
				}

				jsonBody := extractJSONBody(headers, body)
				if jsonBody == nil {
					log.Debug("could not extract JSON body from webhook")
					return
				}

				data, err := processor.process(ctx, typesToStream, jsonBody)
				if err != nil {
					log.Error("error processing webhook event", "event", eventType, "error", err.Error())
					return
				}

				for _, d := range data {
					results <- d
				}
			}(ctx)

			return nil
		},
	}, nil
}

// verifySignature checks the HMAC-SHA256 signature of the body.
// The signature header value is expected in the format "sha256=<hex>".
func verifySignature(body []byte, signatureHeader, secret string) bool {
	const prefix = "sha256="
	if !strings.HasPrefix(signatureHeader, prefix) {
		return false
	}

	signatureHex := signatureHeader[len(prefix):]
	signature, err := hex.DecodeString(signatureHex)
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := mac.Sum(nil)

	return hmac.Equal(signature, expected)
}

// extractJSONBody extracts the JSON payload from the webhook body.
// For application/json, the body is returned as-is.
// For application/x-www-form-urlencoded, the "payload" form field is extracted.
func extractJSONBody(headers http.Header, body []byte) []byte {
	contentType := headers.Get("Content-Type")

	if strings.HasPrefix(contentType, "application/json") {
		return body
	}

	if strings.HasPrefix(contentType, "application/x-www-form-urlencoded") {
		values, err := url.ParseQuery(string(body))
		if err != nil {
			return nil
		}
		payload := values.Get("payload")
		if payload == "" {
			return nil
		}
		// Validate that the payload is valid JSON
		if !json.Valid([]byte(payload)) {
			return nil
		}
		return []byte(payload)
	}

	return nil
}
