// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package bitbucket

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

// GetWebhook implements source.WebhookSource. It validates the webhook
// configuration and returns a Webhook that verifies HMAC-SHA256 signatures
// and dispatches events to the processor registry.
func (s *Source) GetWebhook(ctx context.Context, typesToStream map[string]source.Extra, results chan<- source.Data) (source.Webhook, error) {
	if s.webhookConfig.WebhookSecret == "" {
		return source.Webhook{}, fmt.Errorf("%w: %w: %s", ErrBitbucketSource, ErrMissingEnvVariable, "BITBUCKET_WEBHOOK_SECRET")
	}

	return source.Webhook{
		Method: http.MethodPost,
		Path:   s.webhookConfig.WebhookPath,
		Handler: func(ctx context.Context, headers http.Header, body []byte) error {
			log := logger.FromContext(ctx).WithName(loggerName)

			signature := headers.Get("X-Hub-Signature")
			if signature == "" {
				err := fmt.Errorf("%w: missing X-Hub-Signature header", ErrBitbucketSource)
				log.Error("webhook request missing signature header", "error", err.Error())
				return err
			}

			if !verifySignature(body, signature, s.webhookConfig.WebhookSecret) {
				err := fmt.Errorf("%w: invalid webhook signature", ErrBitbucketSource)
				log.Error("webhook request with invalid signature", "error", err.Error())
				return err
			}

			go func() {
				eventType := headers.Get(bitbucketEventHeader)
				processor, ok := eventProcessors[eventType]
				if !ok {
					log.Debug("ignoring unsupported event", bitbucketEventHeader, eventType)
					return
				}

				data, err := processor.process(ctx, s.client, typesToStream, body)
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
