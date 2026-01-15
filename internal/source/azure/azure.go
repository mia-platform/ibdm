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
		var query *string
		switch resType {
		case arm.ResourceGroupResourceType.String():
			graphResourceType := arm.SubscriptionResourceType.String() + "/resourceGroups"
			query = to.Ptr(fmt.Sprintf(resourceContainerGraphQueryTemplate, graphResourceType))
		case arm.SubscriptionResourceType.String():
			query = to.Ptr(fmt.Sprintf(resourceContainerGraphQueryTemplate, resType))
		default:
			query = to.Ptr(fmt.Sprintf(resourceGraphQueryTemplate, resType))
		}

		queryRequest := armresourcegraph.QueryRequest{
			Subscriptions: []*string{to.Ptr(s.SubscriptionID)},
			Query:         query,
		}

		for {
			timestamp := timeProvider()
			response, err := client.Resources(ctx, queryRequest, nil)

			switch {
			case errors.Is(err, context.Canceled):
				logger.Debug("stopping sync process due to context cancellation")
				return nil
			case err != nil:
				return handleError(err)
			}

			if data, ok := response.Data.([]any); ok {
				for _, item := range data {
					if values, ok := item.(map[string]any); ok {
						values["type"] = resType // ensure type is case-normalized, and resourceGroup is normalized too
						dataChannel <- source.Data{
							Type:      resType,
							Operation: source.DataOperationUpsert,
							Time:      timestamp,
							Values:    values,
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
				break
			}

			queryRequest.Options = &armresourcegraph.QueryRequestOptions{
				SkipToken: response.SkipToken,
			}
		}
	}

	return nil
}

// StartEventStream implement source.EventSource.
func (s *Source) StartEventStream(ctx context.Context, typesToFilter map[string]source.Extra, dataChannel chan<- source.Data) error {
	if err := s.validateForEventStream(); err != nil {
		return handleError(err)
	}

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
	return handleError(processor.Run(ctx))
}

func partitionEventHandler(client *armresources.Client, typesToFilter map[string]source.Extra, dataChannel chan<- source.Data) eventHandler {
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
			resID, err := resourceIDFromSubject(envelope.Subject)
			if err != nil {
				logger.Error("failed to parse resource ID from subject", "error", err.Error(), "subject", envelope.Subject)
				continue
			}

			if envelope.Subject != nil && filterBasedOnResourceID(resID, typesSlice) {
				logger.Debug("skipping event based on type", "resourceID", resID.ResourceType.String())
				continue
			}

			apiVersion, ok := typesToFilter[resID.ResourceType.String()]["apiVersion"].(string)
			if !ok {
				logger.Debug("skipping event with missing apiVersion", "resourceID", resID.ResourceType.String())
				continue
			}

			logger.Trace("handling resource", "resourceID", resID.ResourceType.String(), "eventType", envelope.Type, "apiVersion", apiVersion)
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

				dataChannel <- source.Data{
					Type:      resID.ResourceType.String(),
					Operation: source.DataOperationUpsert,
					Time:      *envelope.Time,
					Values:    values,
				}
			case azsystemevents.TypeResourceDeleteSuccess:
				logger.Trace("we have to delete something", "resourceID", resID.ResourceType.String())
				dataChannel <- source.Data{
					Type:      resID.ResourceType.String(),
					Operation: source.DataOperationDelete,
					Time:      *envelope.Time,
					Values: map[string]any{
						"id":   resID.String(),
						"type": resID.ResourceType.String(),
					},
				}
			default:
				logger.Trace("skipping resource", "resourceID", resID.ResourceType.String(), "eventType", envelope.Type, "apiVersion", apiVersion)
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

// filterBasedOnResourceID checks if the resource type is in the typesToFilter slice.
func filterBasedOnResourceID(resID *arm.ResourceID, typesToFilter []string) bool {
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
