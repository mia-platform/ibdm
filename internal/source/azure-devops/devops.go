// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package azuredevops

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/caarlos0/env/v11"

	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/mia-platform/ibdm/internal/source"
)

var (
	ErrDevOpsSource = errors.New("azure devops source")

	ErrNoAuthenticationHeaderFound        = errors.New("no authentication header found in request")
	ErrMultipleAuthenticationHeadersFound = errors.New("multiple authentication headers found in request")
	ErrInvalidAuthenticationType          = errors.New("invalid authentication type in request")
	ErrUnauthorized                       = errors.New("unauthorized request")

	ErrInvalidWebhookPayload = errors.New("invalid webhook payload")
)

const (
	logName = "ibdm:source:azuredevops"

	extraEventNamesKey = "eventNames"
)

var _ source.SyncableSource = &Source{}
var _ source.WebhookSource = &Source{}
var _ source.ClosableSource = &Source{}

// Source implement both source.WebhookSource and source.SyncableSource for Azure DevOps.
type Source struct {
	config

	syncContext atomic.Pointer[processContext]
	syncLock    sync.Mutex
}

// processContext holds references needed for a sync process lifecycle.
type processContext struct {
	cancel context.CancelFunc
}

// NewSource creates a new Azure DevOps Source reading the needed configuration from the env variables.
func NewSource() (*Source, error) {
	config, err := env.ParseAs[config]()
	if err != nil {
		return nil, handleErr(err)
	}

	return &Source{
		config: config,
	}, nil
}

// StartSyncProcess implement source.SyncableSource interface.
func (s *Source) StartSyncProcess(ctx context.Context, typesToFilter map[string]source.Extra, dataChannel chan<- source.Data) error {
	log := logger.FromContext(ctx).WithName(logName)
	if !s.syncLock.TryLock() {
		log.Debug("sync process already running")
		return nil
	}
	defer s.syncLock.Unlock()

	if err := s.validateForSync(); err != nil {
		return handleErr(err)
	}

	client, err := s.client()
	if err != nil {
		return handleErr(err)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	s.syncContext.Store(&processContext{
		cancel: cancel,
	})

	err = syncResources(ctx, client, typesToFilter, dataChannel)
	s.syncContext.Store(nil)
	return handleErr(err)
}

// GetWebhook implement source.WebhookSource interface.
func (s *Source) GetWebhook(ctx context.Context, typesToStream map[string]source.Extra, results chan<- source.Data) (source.Webhook, error) {
	if err := s.validateForWebhook(); err != nil {
		return source.Webhook{}, handleErr(err)
	}

	return source.Webhook{
		Method:  http.MethodPost,
		Path:    s.WebhookPath,
		Handler: s.webhookHandler(typesToStream, results),
	}, nil
}

func extractDataFromPayload(payload map[string]any) (string, map[string]any, time.Time, error) {
	eventType, found := payload["eventType"]
	if !found {
		return "", nil, time.Time{}, fmt.Errorf("%w: missing eventType", ErrInvalidWebhookPayload)
	}

	resource, ok := payload["resource"].(map[string]any)
	if !ok {
		return "", nil, time.Time{}, fmt.Errorf("%w: invalid resource field", ErrInvalidWebhookPayload)
	}

	operationTime := timeSource()
	timeStamp, ok := resource["utcTimestamp"].(string)
	if ok {
		if parsedTime, err := time.Parse(time.RFC3339Nano, timeStamp); err == nil {
			operationTime = parsedTime
		}
	}

	eventTypeStr, ok := eventType.(string)
	if !ok {
		return "", nil, time.Time{}, fmt.Errorf("%w: eventType is not a string", ErrInvalidWebhookPayload)
	}

	return eventTypeStr, resource, operationTime, nil
}

func (s *Source) webhookHandler(typesToStream map[string]source.Extra, dataChannel chan<- source.Data) source.WebhookHandler {
	return func(ctx context.Context, headers http.Header, body []byte) error {
		log := logger.FromContext(ctx).WithName(logName)
		log.Trace("received webhook from Azure DevOps")
		if err := s.webhookValidation(headers); err != nil {
			log.Debug("webhook failed authentication", "error", err)
			return handleErr(err)
		}

		go func(log logger.Logger, body []byte, typesToStream map[string]source.Extra, _ chan<- source.Data) {
			var payload map[string]any
			if err := json.Unmarshal(body, &payload); err != nil {
				log.Error("failed to unmarshal webhook payload", "error", err)
				return
			}

			eventType, resource, operationTime, err := extractDataFromPayload(payload)
			if err != nil {
				log.Error("failed to extract data from webhook payload", "error", err)
				return
			}

			for typeString, extra := range typesToStream {
				if eventTypes, ok := extra[extraEventNamesKey]; ok {
					if eventTypesList, ok := eventTypes.([]string); ok {
						for _, event := range eventTypesList {
							if strings.EqualFold(eventType, event) {
								log.Debug("webhook handled", "webhookType", eventType, "resourceType", typeString)
								if strings.EqualFold(typeString, "gitrepository") {
									if repo, ok := resource["repository"].(map[string]any); ok {
										resource = repo
									}
								}

								operation := source.DataOperationUpsert
								if strings.HasSuffix(eventType, ".deleted") {
									operation = source.DataOperationDelete
								}
								dataChannel <- source.Data{
									Type:      typeString,
									Operation: operation,
									Time:      operationTime,
									Values:    resource,
								}
							}
						}
					}
				}
			}

			log.Trace("webhook event type not configured to be streamed", "eventType", eventType)
		}(log, body, typesToStream, dataChannel)
		return nil
	}
}

func (s *Source) webhookValidation(headers http.Header) error {
	authHeader, found := headers["Authorization"]
	switch {
	case !found && len(s.WebhookUser) == 0 && len(s.WebhookPassword) == 0:
		return nil
	case !found:
		return fmt.Errorf("%w: %w", ErrNoAuthenticationHeaderFound, ErrUnauthorized)
	}

	if len(authHeader) > 1 {
		return fmt.Errorf("%w: %w", ErrMultipleAuthenticationHeadersFound, ErrUnauthorized)
	}
	parts := strings.Fields(authHeader[0])
	if len(parts) != 2 || !strings.EqualFold(parts[0], "basic") {
		return fmt.Errorf("%w: %w", ErrInvalidAuthenticationType, ErrUnauthorized)
	}

	buffer := new(bytes.Buffer)
	fmt.Fprintf(buffer, "%s:%s", s.WebhookUser, s.WebhookPassword)
	expectedAuthentication := base64.StdEncoding.AppendEncode([]byte{}, buffer.Bytes())
	expectedAuthenticationHash := sha256.Sum256(expectedAuthentication)
	authenticationHash := sha256.Sum256([]byte(parts[1]))
	if subtle.ConstantTimeCompare(expectedAuthenticationHash[:], authenticationHash[:]) == 1 {
		return nil
	}

	return ErrUnauthorized
}

// Close implement source.ClosableSource interface.
func (s *Source) Close(ctx context.Context, _ time.Duration) error {
	log := logger.FromContext(ctx).WithName(logName)
	log.Debug("closing Microsoft Azure client")

	syncClient := s.syncContext.Swap(nil)
	if syncClient != nil {
		log.Debug("cancelling sync process")
		syncClient.cancel()
	}

	log.Trace("closed Microsoft Azure client")
	return nil
}

// handleError always wraps the given error with ErrAzureDevOpsSource.
// It also unwraps some errors to cleanup the error message and removing unnecessary layers.
func handleErr(err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, context.Canceled) {
		return nil
	}

	return fmt.Errorf("%w: %w", ErrDevOpsSource, err)
}
