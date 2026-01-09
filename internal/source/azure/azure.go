// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package azure

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/messaging"
	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azeventhubs/v2"
	"github.com/Azure/azure-sdk-for-go/sdk/messaging/eventgrid/azsystemevents"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources/v3"
	"github.com/caarlos0/env/v11"

	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/mia-platform/ibdm/internal/source"
)

type eventHandler func(context.Context, *azeventhubs.ReceivedEventData)

var (
	// ErrAzureSource is the sentinel error for all Azure Source errors.
	ErrAzureSource = errors.New("azure source")
)

const (
	logName = "ibdm:source:azure"
)

var _ source.SyncableSource = &Source{}
var _ source.EventSource = &Source{}
var _ source.ClosableSource = &Source{}

// Source implement both source.StreamableSource and source.SyncableSource for Azure.
type Source struct {
	config

	eventStreamContext atomic.Pointer[processContext]

	syncLock    sync.Mutex
	syncContext atomic.Pointer[processContext]
}

// processContext holds references needed for a sync process lifecycle.
type processContext struct {
	cancel context.CancelFunc
}

// NewSource creates a new Azure Source reading the needed configuration from the env variables.
func NewSource() (*Source, error) {
	config, err := env.ParseAs[config]()
	if err != nil {
		return nil, handleError(err)
	}

	return &Source{
		config: config,
	}, nil
}

// StartSyncProcess implement source.SyncableSource.
func (s *Source) StartSyncProcess(ctx context.Context, _ map[string]source.Extra, _ chan<- source.Data) error {
	log := logger.FromContext(ctx).WithName(logName)
	if !s.syncLock.TryLock() {
		log.Debug("sync process already running")
		return nil
	}
	defer s.syncLock.Unlock()

	if err := s.validateForSync(); err != nil {
		return handleError(err)
	}

	return nil
}

// StartEventStream implement source.EventSource.
func (s *Source) StartEventStream(ctx context.Context, typesToFilter map[string]source.Extra, dataChannel chan<- source.Data) error {
	if err := s.validateForEventStream(); err != nil {
		return handleError(err)
	}

	eventHubClient, err := s.newEventHubClient()
	if err != nil {
		return handleError(err)
	}
	defer eventHubClient.Close(ctx)

	checkpointClient, err := s.newCheckpointClient()
	if err != nil {
		return handleError(err)
	}

	processor, err := azeventhubs.NewProcessor(eventHubClient, checkpointClient, nil)
	if err != nil {
		return handleError(err)
	}

	clientFactory, err := s.azureClientFactory()
	if err != nil {
		return handleError(err)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	s.eventStreamContext.Store(&processContext{
		cancel: cancel,
	})

	eventHandler := partitionEventHandler(clientFactory, typesToFilter, dataChannel)
	go startPartitionClients(ctx, processor, eventHandler)
	return handleError(processor.Run(ctx))
}

func partitionEventHandler(_ *armresources.ClientFactory, typesToFilter map[string]source.Extra, _ chan<- source.Data) eventHandler {
	typesSlice := make([]string, 0, len(typesToFilter))
	for t := range typesToFilter {
		typesSlice = append(typesSlice, t)
	}

	return func(ctx context.Context, receivedData *azeventhubs.ReceivedEventData) {
		logger := logger.FromContext(ctx).WithName(logName)
		cloudEvents := make([]messaging.CloudEvent, 0)
		if err := json.Unmarshal(receivedData.Body, &cloudEvents); err != nil {
			logger.Error("failed to unmarshal received event data", "error", err.Error())
			return
		}

		for _, envelope := range cloudEvents {
			if envelope.Subject != nil && filterBasedOnResourceURI(*envelope.Subject, typesSlice) {
				logger.Trace("skipping event based on type", "subject", *envelope.Subject)
				continue
			}

			switch envelope.Type {
			case azsystemevents.TypeResourceWriteSuccess:

			case azsystemevents.TypeResourceDeleteSuccess:

			default:
				logger.Trace("skipping event with unhandled type", "type", envelope.Type)
			}
		}
	}
}

func filterBasedOnResourceURI(resourceURI string, typesToFilter []string) bool {
	// Example resource URI: /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/{resourceProviderNamespace}/{resourceType}/{resourceName}
	resID, err := arm.ParseResourceID(resourceURI)
	if err != nil { // if we can't parse it, filter it out
		return true
	}

	resourceType := resID.ResourceType.String()
	return !slices.ContainsFunc(typesToFilter, func(s string) bool {
		return strings.EqualFold(s, resourceType)
	})
}

// Close implement source.ClosableSource.
func (s *Source) Close(ctx context.Context, _ time.Duration) error {
	log := logger.FromContext(ctx).WithName(logName)
	log.Debug("closing Microsoft Azure client")

	syncClient := s.syncContext.Swap(nil)
	if syncClient != nil {
		syncClient.cancel()
	}

	eventStreamClient := s.eventStreamContext.Swap(nil)
	if eventStreamClient != nil {
		eventStreamClient.cancel()
	}

	log.Trace("closed Microsoft Azure client")
	return nil
}

// handleError always wraps the given error with ErrAzureSource.
// It also unwraps some errors to cleanup the error message and removing unnecessary layers.
func handleError(err error) error {
	var parseErr env.AggregateError
	if errors.As(err, &parseErr) {
		err = parseErr.Errors[0]
	}

	if errors.Is(err, context.Canceled) {
		return nil
	}

	return fmt.Errorf("%w: %w", ErrAzureSource, err)
}

func startPartitionClients(ctx context.Context, processor *azeventhubs.Processor, handleEvent eventHandler) {
	logger := logger.FromContext(ctx).WithName(logName)
	for {
		partitionClient := processor.NextPartitionClient(ctx)
		if partitionClient == nil {
			break
		}

		go func(ctx context.Context, pc *azeventhubs.ProcessorPartitionClient) {
			defer pc.Close(ctx)
			logger.Trace("starting partition client", "partitionID", partitionClient.PartitionID())

			for {
				receiveCtx, cancelReceive := context.WithTimeout(ctx, 30*time.Second)
				events, err := pc.ReceiveEvents(receiveCtx, 10, nil)
				cancelReceive()

				if err != nil && !errors.Is(err, context.DeadlineExceeded) {
					var eventHubError *azeventhubs.Error
					if errors.As(err, &eventHubError) && eventHubError.Code == azeventhubs.ErrorCodeOwnershipLost {
						logger.Error("closing partition client for ownership lost", "partitionID", pc.PartitionID())
						return
					}

					logger.Error("partition client receive failed", "error", err.Error(), "partitionID", pc.PartitionID())
					return
				}

				for _, event := range events {
					handleEvent(ctx, event)
					if err := partitionClient.UpdateCheckpoint(ctx, event, nil); err != nil && !errors.Is(err, context.Canceled) {
						logger.Error("failed to update checkpoint", "error", err.Error(), "partitionID", pc.PartitionID())
					}
				}
			}
		}(ctx, partitionClient)
	}
}
