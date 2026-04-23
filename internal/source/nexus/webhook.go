// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package nexus

import (
	"context"
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // Nexus webhook HMAC signatures use SHA1 (upstream protocol requirement)
	"encoding/hex"
	"fmt"
	"net/http"

	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/mia-platform/ibdm/internal/source"
)

const (
	// nexusSignatureHeader is the HTTP header carrying the HMAC-SHA1 digest.
	nexusSignatureHeader = "X-Nexus-Webhook-Signature"
)

// GetWebhook implements source.WebhookSource. It returns a Webhook that optionally
// verifies HMAC-SHA1 signatures (when NEXUS_WEBHOOK_SECRET is configured) and
// dispatches component events to the processor registry.
func (s *Source) GetWebhook(_ context.Context, typesToStream map[string]source.Extra, results chan<- source.Data) (source.Webhook, error) {
	return source.Webhook{
		Method: http.MethodPost,
		Path:   s.webhookConfig.WebhookPath,
		Handler: func(ctx context.Context, headers http.Header, body []byte) error {
			log := logger.FromContext(ctx).WithName(loggerName)

			if s.webhookConfig.WebhookSecret != "" {
				signature := headers.Get(nexusSignatureHeader)
				if signature == "" {
					err := fmt.Errorf("%w: missing %s header", ErrNexusSource, nexusSignatureHeader)
					log.Error("webhook request missing signature header", "error", err.Error())
					return err
				}

				if !verifySignature(body, signature, s.webhookConfig.WebhookSecret) {
					err := fmt.Errorf("%w: invalid webhook signature", ErrNexusSource)
					log.Error("webhook request with invalid signature", "error", err.Error())
					return err
				}
			}

			go func() {
				eventType := headers.Get(nexusEventHeader)
				processor, ok := eventProcessors[eventType]
				if !ok {
					log.Debug("ignoring unsupported event", nexusEventHeader, eventType)
					return
				}

				data, err := processor.process(ctx, s.client, s.config.URLHost, typesToStream, body)
				if err != nil {
					log.Error("error processing webhook event", "event", eventType, "error", err.Error())
					return
				}

				for _, d := range data {
					results <- d
				}
			}()

			return nil
		},
	}, nil
}

// verifySignature checks the HMAC-SHA1 signature of body.
// The Nexus signature header contains the raw hex-encoded digest without any prefix.
// It uses hmac.Equal for constant-time comparison to prevent timing attacks.
func verifySignature(body []byte, signature, secret string) bool {
	decoded, err := hex.DecodeString(signature)
	if err != nil {
		return false
	}

	mac := hmac.New(sha1.New, []byte(secret))
	mac.Write(body)
	expected := mac.Sum(nil)

	return hmac.Equal(decoded, expected)
}
