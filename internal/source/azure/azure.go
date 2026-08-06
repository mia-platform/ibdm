// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package azure

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/messaging"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azeventhubs/v2"
	"github.com/Azure/azure-sdk-for-go/sdk/messaging/eventgrid/azsystemevents"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resourcegraph/armresourcegraph"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources/v3"
	"github.com/caarlos0/env/v11"

	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/mia-platform/ibdm/internal/source"
)

type eventHandler func(context.Context, *azeventhubs.ReceivedEventData)

var (
	// ErrAzureSource is the sentinel error for all Azure Source errors.
	ErrAzureSource = errors.New("azure source")
	timeProvider   = time.Now
)

const (
	logName = "ibdm:source:azure"

	resourceGraphQueryTemplate = `resources |
	where type =~ '%s' |
	project extendedLocation,identity,kind,location,managedBy,plan,properties,sku,tags,id,name,type
	`
	resourceContainerGraphQueryTemplate = `resourcecontainers |
	where type =~ '%s' |
	project extendedLocation,identity,kind,location,managedBy,plan,properties,sku,tags,id,name,type
	`
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

	azureCredentials, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, handleError(err)
	}
	config.azureCredentials = azureCredentials

	return &Source{
		config: config,
	}, nil
}

// StartSyncProcess implement source.SyncableSource.
func (s *Source) StartSyncProcess(ctx context.Context, typesToFilter map[string]source.Extra, dataChannel chan<- source.Data) error {
	logger := logger.FromContext(ctx).WithName(logName)
	if !s.syncLock.TryLock() {
		logger.Debug("sync process already running")
		return nil
	}
	defer s.syncLock.Unlock()

	if err := s.validateForSync(); err != nil {
		return handleError(err)
	}
	warnOrphanSubTypes(logger, typesToFilter)

	client, err := s.azureGraphClient()
	if err != nil {
		return handleError(err)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	s.syncContext.Store(&processContext{
		cancel: cancel,
	})

	for resType := range typesToFilter {
		// a sub-type is emitted while handling its parent resource, and its type key is an internal
		// dispatch key: querying Azure for it would only ask for a type that does not exist.
		if isSubTypeKey(resType) {
			logger.Debug("skipping sub-type mapping, it is emitted with its parent type", "type", resType)
			continue
		}

		if err := s.syncResourceType(ctx, client, resType, typesToFilter, dataChannel); err != nil {
			// handleError swallows the cancellation, so a stopped sync process is not a failure
			return handleError(err)
		}
	}

	s.syncContext.Swap(nil)
	return nil
}

// syncResourceType pages through every resource of resType the Resource Graph returns and emits
// the item of each one of them, together with the ones of the sub-types they additionally produce.
func (s *Source) syncResourceType(ctx context.Context, client *armresourcegraph.Client, resType string, typesToFilter map[string]source.Extra, dataChannel chan<- source.Data) error {
	logger := logger.FromContext(ctx).WithName(logName)
	queryRequest := armresourcegraph.QueryRequest{
		Subscriptions: []*string{to.Ptr(s.SubscriptionID)},
		Query:         resourceGraphQuery(resType),
	}

	for {
		timestamp := timeProvider()
		response, err := client.Resources(ctx, queryRequest, nil)

		switch {
		case errors.Is(err, context.Canceled):
			logger.Debug("stopping sync process due to context cancellation")
			return err
		case err != nil:
			return err
		}

		if data, ok := response.Data.([]any); ok {
			for _, item := range data {
				if values, ok := item.(map[string]any); ok {
					normalizeResourceValues(logger, values, resType)
					for _, resourceData := range resourceDataToEmit(resType, values, typesToFilter, source.DataOperationUpsert, timestamp) {
						dataChannel <- resourceData
					}
				} else {
					// something very wrong is going on, print an error and continue
					logger.Debug("retrieve data item is not a valid map")
				}
			}
		} else {
			// something very wrong is going on, print an error and continue
			logger.Debug("response data is not a valid type")
		}

		if response.ResultTruncated == nil || *response.ResultTruncated == armresourcegraph.ResultTruncatedFalse {
			return nil
		}

		queryRequest.Options = &armresourcegraph.QueryRequestOptions{
			SkipToken: response.SkipToken,
		}
	}
}

// resourceGraphQuery returns the Resource Graph query retrieving every resource of resType, taken
// from the container table for the types that live in it.
func resourceGraphQuery(resType string) *string {
	switch resType {
	case arm.ResourceGroupResourceType.String():
		graphResourceType := arm.SubscriptionResourceType.String() + "/resourceGroups"
		return to.Ptr(fmt.Sprintf(resourceContainerGraphQueryTemplate, graphResourceType))
	case arm.SubscriptionResourceType.String():
		return to.Ptr(fmt.Sprintf(resourceContainerGraphQueryTemplate, resType))
	default:
		return to.Ptr(fmt.Sprintf(resourceGraphQueryTemplate, resType))
	}
}

// StartEventStream implement source.EventSource.
func (s *Source) StartEventStream(ctx context.Context, typesToFilter map[string]source.Extra, dataChannel chan<- source.Data) error {
	logger := logger.FromContext(ctx).WithName(logName)
	if err := s.validateForEventStream(); err != nil {
		return handleError(err)
	}
	warnOrphanSubTypes(logger, typesToFilter)

	client, err := s.azureClient()
	if err != nil {
		return handleError(err)
	}

	eventHubClient, processor, err := s.setupEventStreamProcessors()
	if err != nil {
		return handleError(err)
	}
	defer eventHubClient.Close(ctx)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	s.eventStreamContext.Store(&processContext{
		cancel: cancel,
	})

	eventHandler := partitionEventHandler(client, typesToFilter, dataChannel)
	go startPartitionClients(ctx, processor, eventHandler)
	logger.Debug("starting azure event hub processor")
	err = processor.Run(ctx)
	s.eventStreamContext.Swap(nil)
	return handleError(err)
}

func partitionEventHandler(client *armresources.Client, typesToFilter map[string]source.Extra, dataChannel chan<- source.Data) eventHandler {
	// a sub-type type key is an internal dispatch key and can never be the type of an event
	// subject, so it is left out of the set the subject type is resolved against.
	typesSlice := slices.DeleteFunc(slices.Sorted(maps.Keys(typesToFilter)), isSubTypeKey)

	return func(ctx context.Context, receivedData *azeventhubs.ReceivedEventData) {
		logger := logger.FromContext(ctx).WithName(logName)
		cloudEvents := make([]messaging.CloudEvent, 0)
		if err := json.Unmarshal(receivedData.Body, &cloudEvents); err != nil {
			logger.Error("failed to unmarshal received event data", "error", err.Error())
			return
		}

		for _, envelope := range cloudEvents {
			resID, err := resourceIDFromSubject(envelope.Subject)
			if err != nil {
				logger.Error("failed to parse resource ID from subject", "error", err.Error(), "subject", envelope.Subject)
				continue
			}

			// the subject can spell the resource type with any casing, so resolve the configured
			// key once and use it for the apiVersion lookup and for every emitted value.
			resourceType, ok := configuredResourceType(typesSlice, resID.ResourceType.String())
			if !ok {
				logger.Debug("skipping event based on type", "resourceType", resID.ResourceType.String())
				continue
			}

			apiVersion, ok := typesToFilter[resourceType][apiVersionKey].(string)
			if !ok {
				logger.Debug("skipping event with missing apiVersion", "resourceType", resourceType)
				continue
			}

			logger.Trace("handling resource", "resourceType", resourceType, "eventType", envelope.Type, "apiVersion", apiVersion)
			switch envelope.Type {
			case azsystemevents.TypeResourceWriteSuccess:
				logger.Trace("request resource data from azure", "resourceID", *envelope.Subject)
				response, err := client.GetByID(ctx, resID.String(), apiVersion, nil)
				switch {
				case errors.Is(err, context.Canceled):
					logger.Debug("stopping processing due to context cancellation")
					continue
				case err != nil:
					logger.Error("failed to get resource from Azure", "error", err.Error(), "resourceID", *envelope.Subject)
					continue
				}

				values, err := unmarshalAzureResponse(response.GenericResource)
				if err != nil {
					logger.Error("failed to unmarshal resource from Azure", "error", err.Error(), "resourceID", *envelope.Subject)
					continue
				}

				normalizeResourceValues(logger, values, resourceType)
				for _, resourceData := range resourceDataToEmit(resourceType, values, typesToFilter, source.DataOperationUpsert, *envelope.Time) {
					dataChannel <- resourceData
				}
			case azsystemevents.TypeResourceDeleteSuccess:
				logger.Trace("deleting resource", "resourceType", resourceType)
				// the event carries only the resource id, so no sub-type check can run here and a
				// delete is emitted for every configured sub-type of the resource type.
				values := map[string]any{idKey: resID.String()}
				normalizeResourceValues(logger, values, resourceType)
				for _, resourceData := range resourceDataToEmit(resourceType, values, typesToFilter, source.DataOperationDelete, *envelope.Time) {
					dataChannel <- resourceData
				}
			default:
				logger.Trace("skipping resource", "resourceType", resourceType, "eventType", envelope.Type, "apiVersion", apiVersion)
			}
		}
	}
}

// unmarshalAzureResponse converts an armresources.ClientGetByIDResponse to a map[string]any.
func unmarshalAzureResponse(res armresources.GenericResource) (map[string]any, error) {
	data, err := res.MarshalJSON()
	if err != nil {
		return nil, err
	}

	var values map[string]any
	if err := json.Unmarshal(data, &values); err != nil {
		return nil, err
	}
	return values, nil
}

// resourceIDFromSubject parses a resource ID from a subject string.
func resourceIDFromSubject(subject *string) (*arm.ResourceID, error) {
	if subject == nil {
		return nil, errors.New("subject is nil")
	}

	// Example resource URI: /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/{resourceProviderNamespace}/{resourceType}/{resourceName}
	return arm.ParseResourceID(*subject)
}

// Close implement source.ClosableSource.
func (s *Source) Close(ctx context.Context, _ time.Duration) error {
	log := logger.FromContext(ctx).WithName(logName)
	log.Debug("closing Microsoft Azure client")

	syncClient := s.syncContext.Swap(nil)
	if syncClient != nil {
		log.Debug("cancelling sync process")
		syncClient.cancel()
	}

	eventStreamClient := s.eventStreamContext.Swap(nil)
	if eventStreamClient != nil {
		log.Debug("cancelling event stream process")
		eventStreamClient.cancel()
	}

	log.Trace("closed Microsoft Azure client")
	return nil
}

// handleError always wraps the given error with ErrAzureSource.
// It also unwraps some errors to cleanup the error message and removing unnecessary layers.
func handleError(err error) error {
	if err == nil {
		return nil
	}

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

				switch {
				case errors.Is(err, context.Canceled):
					logger.Debug("stopping partition client due to context cancellation", "partitionID", pc.PartitionID())
					return
				case err != nil && !errors.Is(err, context.DeadlineExceeded):
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
