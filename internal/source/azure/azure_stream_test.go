// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package azure

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	fakeazcore "github.com/Azure/azure-sdk-for-go/sdk/azcore/fake"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azeventhubs/v2"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources/v3"
	fakearmresources "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources/v3/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/source"
)

func TestInvalidEventStreamProcess(t *testing.T) {
	t.Parallel()

	azureSource := &Source{
		config: config{},
	}

	err := azureSource.StartEventStream(t.Context(), nil, nil)
	assert.ErrorIs(t, err, ErrMissingEnvVariable)
}

func TestCancelledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	azureSource := &Source{
		config: config{
			SubscriptionID:             "00000000-0000-0000-0000-000000000000",
			EventHubConnectionString:   "Endpoint=sb://example.servicebus.windows.net/;SharedAccessKeyName=keyname;SharedAccessKey=keyvalue;EntityPath=eventhubname",
			CheckpointConnectionString: "BlobEndpoint=https://account-name.blob.core.windows.net/container;SharedAccessSignature=signature",
		},
	}

	err := azureSource.StartEventStream(ctx, nil, nil)
	assert.ErrorIs(t, ctx.Err(), context.Canceled)
	assert.NoError(t, err)
}

func TestPartitionEventHandler(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		contextFunc   func(tb testing.TB) (context.Context, context.CancelFunc)
		typesToFilter map[string]source.Extra
		azureData     *azeventhubs.ReceivedEventData
		expectedData  []source.Data
	}{
		"no events": {
			contextFunc: func(tb testing.TB) (context.Context, context.CancelFunc) {
				tb.Helper()
				return context.WithTimeout(tb.Context(), 1*time.Second)
			},
			typesToFilter: map[string]source.Extra{
				"Microsoft.Resources/resourceGroups": {"apiVersion": "2021-04-01"},
				"Microsoft.Compute/virtualMachines":  {"apiVersion": "2021-07-01"},
			},
			azureData: &azeventhubs.ReceivedEventData{
				EventData: azeventhubs.EventData{
					Body: json.RawMessage(`[]`),
				},
			},
		},
		"multiple valid events": {
			contextFunc: func(tb testing.TB) (context.Context, context.CancelFunc) {
				tb.Helper()
				return context.WithTimeout(tb.Context(), 1*time.Second)
			},
			typesToFilter: map[string]source.Extra{
				"Microsoft.Resources/resourceGroups": {"apiVersion": "2021-04-01"},
				"Microsoft.Compute/virtualMachines":  {"apiVersion": "2021-07-01"},
			},
			azureData: &azeventhubs.ReceivedEventData{
				EventData: azeventhubs.EventData{
					Body: eventDataResourcesTestBody,
				},
			},
			expectedData: eventDataResourcesExpectedData,
		},
		"missing apiVersion": {
			contextFunc: func(tb testing.TB) (context.Context, context.CancelFunc) {
				tb.Helper()
				return context.WithTimeout(tb.Context(), 1*time.Second)
			},
			typesToFilter: map[string]source.Extra{
				"Microsoft.Resources/resourceGroups": {},
			},
			azureData: &azeventhubs.ReceivedEventData{
				EventData: azeventhubs.EventData{
					Body: json.RawMessage(`[{"id":"00000000-0000-0000-0000-00000-0000000","source":"/subscriptions/00000000-0000-0000-0000-00000-0000000","specversion":"1.0","type":"Microsoft.Resources.ResourceWriteSuccess","subject":"/subscriptions/00000000-0000-0000-0000-00000-0000000/resourceGroups/myResourceGroup"}]`),
				},
			},
		},
		"not a CloudEvent": {
			contextFunc: func(tb testing.TB) (context.Context, context.CancelFunc) {
				tb.Helper()
				return context.WithTimeout(tb.Context(), 1*time.Second)
			},
			azureData: &azeventhubs.ReceivedEventData{
				EventData: azeventhubs.EventData{
					Body: json.RawMessage(`"not a cloudevent"`),
				},
			},
		},
		"missing subject": {
			contextFunc: func(tb testing.TB) (context.Context, context.CancelFunc) {
				tb.Helper()
				return context.WithTimeout(tb.Context(), 1*time.Second)
			},
			typesToFilter: map[string]source.Extra{
				"Microsoft.Resources/resourceGroups": {"apiVersion": "2021-04-01"},
			},
			azureData: &azeventhubs.ReceivedEventData{
				EventData: azeventhubs.EventData{
					Body: json.RawMessage(`[{"id":"00000000-0000-0000-0000-00000-0000000","source":"/subscriptions/00000000-0000-0000-0000-00000-0000000","specversion":"1.0","type":"Microsoft.Resources.ResourceWriteSuccess"}]`),
				},
			},
		},
		"resource provider returning a divergently cased id": {
			contextFunc: func(tb testing.TB) (context.Context, context.CancelFunc) {
				tb.Helper()
				return context.WithTimeout(tb.Context(), 1*time.Second)
			},
			typesToFilter: map[string]source.Extra{
				managedClustersType: {"apiVersion": managedClustersAPIVersion},
			},
			azureData: &azeventhubs.ReceivedEventData{
				EventData: azeventhubs.EventData{
					Body: eventDataManagedClusterWriteBody,
				},
			},
			expectedData: []source.Data{
				{
					Type:      managedClustersType,
					Operation: source.DataOperationUpsert,
					Time:      time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
					Values: map[string]any{
						"id":   normalizedManagedClusterID,
						"type": managedClustersType,
					},
				},
			},
		},
		"non canonical subject type is not dropped": {
			contextFunc: func(tb testing.TB) (context.Context, context.CancelFunc) {
				tb.Helper()
				return context.WithTimeout(tb.Context(), 1*time.Second)
			},
			typesToFilter: map[string]source.Extra{
				managedClustersType: {"apiVersion": managedClustersAPIVersion},
			},
			azureData: &azeventhubs.ReceivedEventData{
				EventData: azeventhubs.EventData{
					Body: eventDataLowerTypeManagedClusterWriteBody,
				},
			},
			expectedData: []source.Data{
				{
					Type:      managedClustersType,
					Operation: source.DataOperationUpsert,
					Time:      time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
					Values: map[string]any{
						"id":   normalizedManagedClusterID,
						"type": managedClustersType,
					},
				},
			},
		},
		"delete with non canonical subject casing": {
			contextFunc: func(tb testing.TB) (context.Context, context.CancelFunc) {
				tb.Helper()
				return context.WithTimeout(tb.Context(), 1*time.Second)
			},
			typesToFilter: map[string]source.Extra{
				managedClustersType: {"apiVersion": managedClustersAPIVersion},
			},
			azureData: &azeventhubs.ReceivedEventData{
				EventData: azeventhubs.EventData{
					Body: eventDataManagedClusterDeleteBody,
				},
			},
			expectedData: []source.Data{
				{
					Type:      managedClustersType,
					Operation: source.DataOperationDelete,
					Time:      time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
					Values: map[string]any{
						"id":   normalizedManagedClusterID,
						"type": managedClustersType,
					},
				},
			},
		},
	}

	for testName, test := range testCases {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := test.contextFunc(t)
			defer cancel()

			client, err := armresources.NewClient("sub-id", &fakeazcore.TokenCredential{}, &arm.ClientOptions{
				ClientOptions: policy.ClientOptions{
					Transport: fakeClientTransport(t),
				},
			})
			require.NoError(t, err)

			dataChannel := make(chan source.Data, 100)
			handler := partitionEventHandler(client, test.typesToFilter, dataChannel)

			handler(ctx, test.azureData)
			close(dataChannel)

			var receivedData []source.Data
			for data := range dataChannel {
				receivedData = append(receivedData, data)
			}

			require.NotErrorIs(t, ctx.Err(), context.DeadlineExceeded)
			// a fake branch keyed on the wrong resource ID silently emits nothing, so assert the
			// received count before comparing the elements
			require.Len(t, receivedData, len(test.expectedData))
			assert.ElementsMatch(t, test.expectedData, receivedData)
		})
	}
}

func fakeClientTransport(tb testing.TB) policy.Transporter {
	tb.Helper()

	return fakearmresources.NewServerTransport(&fakearmresources.Server{
		GetByID: func(_ context.Context, resourceID, apiVersion string, _ *armresources.ClientGetByIDOptions) (responder fakeazcore.Responder[armresources.ClientGetByIDResponse], errResponder fakeazcore.ErrorResponder) {
			switch resp, err := handleResourcesGetByIDRequest(tb, resourceID, apiVersion); {
			case resp != nil:
				responder.SetResponse(http.StatusOK, *resp, nil)
			case err != nil:
				errResponder.SetError(err)
			}

			return responder, errResponder
		},
	})
}

func handleResourcesGetByIDRequest(tb testing.TB, resourceID, apiVersion string) (resp *armresources.ClientGetByIDResponse, err error) {
	tb.Helper()

	switch resourceID {
	case "subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/my-rg":
		assert.Equal(tb, "2021-04-01", apiVersion)
		resp = &armresources.ClientGetByIDResponse{
			GenericResource: armresources.GenericResource{
				ID:   to.Ptr("/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/my-rg"),
				Type: to.Ptr("Microsoft.Resources/resourceGroups"),
			},
		}
		return resp, nil
	case "subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/my-rg/providers/Microsoft.Compute/virtualMachines/my-vm":
		assert.Equal(tb, "2021-07-01", apiVersion)
		return nil, assert.AnError
	case managedClusterGetByIDPath:
		assert.Equal(tb, managedClustersAPIVersion, apiVersion)
		// Microsoft.ContainerService builds its own id form and answers with a lowercase
		// resourcegroups literal even though the request carried the camelCase one
		resp = &armresources.ClientGetByIDResponse{
			GenericResource: armresources.GenericResource{
				ID:   to.Ptr(bodyManagedClusterID),
				Type: to.Ptr("microsoft.containerservice/managedclusters"),
			},
		}
		return resp, nil
	case lowerTypeManagedClusterGetByIDPath:
		assert.Equal(tb, managedClustersAPIVersion, apiVersion)
		resp = &armresources.ClientGetByIDResponse{
			GenericResource: armresources.GenericResource{
				ID:   to.Ptr("/" + lowerTypeManagedClusterGetByIDPath),
				Type: to.Ptr("microsoft.containerservice/managedclusters"),
			},
		}
		return resp, nil
	}

	return nil, nil
}

const (
	managedClustersAPIVersion = "2025-10-01"

	// armresources.Client strips the leading slash before calling the server, and
	// arm.ParseResourceID rewrites the resourcegroups literal of the subject to camelCase while
	// leaving the provider and type segments at the casing the subject used, so these are the keys
	// the fake actually receives.
	managedClusterGetByIDPath          = "subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/my-rg/providers/Microsoft.ContainerService/managedClusters/my-cluster"
	lowerTypeManagedClusterGetByIDPath = "subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/my-rg/providers/microsoft.containerservice/managedclusters/my-cluster"
)

var (
	// eventDataManagedClusterWriteBody carries a lowercase resourcegroups literal and a canonically
	// cased provider and type.
	eventDataManagedClusterWriteBody = json.RawMessage(`[
	{
		"id": "00000000-0000-0000-0000-000000000000",
		"source": "/subscriptions/00000000-0000-0000-0000-000000000000",
		"specversion": "1.0",
		"type": "Microsoft.Resources.ResourceWriteSuccess",
		"subject": "/subscriptions/00000000-0000-0000-0000-000000000000/resourcegroups/my-rg/providers/Microsoft.ContainerService/managedClusters/my-cluster",
		"time": "2020-01-01T00:00:00.0000000Z"
	}]`)

	// eventDataLowerTypeManagedClusterWriteBody spells the provider and the type in lowercase, a
	// casing the configured key does not use.
	eventDataLowerTypeManagedClusterWriteBody = json.RawMessage(`[
	{
		"id": "00000000-0000-0000-0000-000000000000",
		"source": "/subscriptions/00000000-0000-0000-0000-000000000000",
		"specversion": "1.0",
		"type": "Microsoft.Resources.ResourceWriteSuccess",
		"subject": "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/my-rg/providers/microsoft.containerservice/managedclusters/my-cluster",
		"time": "2020-01-01T00:00:00.0000000Z"
	}]`)

	// eventDataManagedClusterDeleteBody spells the resource group literal, the provider and the
	// type in lowercase.
	eventDataManagedClusterDeleteBody = json.RawMessage(`[
	{
		"id": "00000000-0000-0000-0000-000000000000",
		"source": "/subscriptions/00000000-0000-0000-0000-000000000000",
		"specversion": "1.0",
		"type": "Microsoft.Resources.ResourceDeleteSuccess",
		"subject": "/subscriptions/00000000-0000-0000-0000-000000000000/resourcegroups/my-rg/providers/microsoft.containerservice/managedclusters/my-cluster",
		"time": "2020-01-01T00:00:00.0000000Z"
	}]`)

	eventDataResourcesTestBody = json.RawMessage(`[
	{
		"id": "00000000-0000-0000-0000-000000000000",
		"source": "/subscriptions/00000000-0000-0000-0000-000000000000",
		"specversion": "1.0",
		"type": "Microsoft.Resources.ResourceDeleteSuccess",
		"subject": "/subscriptions/00000000-0000-0000-0000-000000000000/resourcegroups/my-rg",
		"time": "2020-01-01T00:00:00.0000000Z",
		"data": {
			"authorization": {},
			"claims": {},
			"correlationId": "00000000-0000-0000-0000-000000000000",
			"httpRequest": {},
			"resourceProvider": "Microsoft.Resources",
			"resourceUri": "/subscriptions/00000000-0000-0000-0000-000000000000/resourcegroups/my-rg",
			"operationName": "Microsoft.Resources/subscriptions/resourcegroups/delete",
			"status": "Succeeded",
			"subscriptionId": "00000000-0000-0000-0000-000000000000",
			"tenantId": "00000000-0000-0000-0000-000000000000"
		}
	},
	{
		"id": "00000000-0000-0000-0000-000000000000",
		"source": "/subscriptions/00000000-0000-0000-0000-000000000000",
		"specversion": "1.0",
		"type": "Microsoft.Resources.ResourceWriteSuccess",
		"subject": "/subscriptions/00000000-0000-0000-0000-000000000000/resourcegroups/my-rg",
		"time": "2020-01-01T00:00:00.0000000Z",
		"data": {
			"authorization": {},
			"claims": {},
			"correlationId": "00000000-0000-0000-0000-000000000000",
			"httpRequest": {},
			"resourceProvider": "Microsoft.Resources",
			"resourceUri": "/subscriptions/00000000-0000-0000-0000-000000000000/resourcegroups/my-rg",
			"operationName": "Microsoft.Resources/subscriptions/resourceGroups/write",
			"status": "Succeeded",
			"subscriptionId": "00000000-0000-0000-0000-000000000000",
			"tenantId": "00000000-0000-0000-0000-000000000000"
		}
	},
	{
		"id": "00000000-0000-0000-0000-000000000000",
		"source": "/subscriptions/00000000-0000-0000-0000-000000000000",
		"specversion": "1.0",
		"type": "Microsoft.Resources.ResourceWriteCancel",
		"subject": "/subscriptions/00000000-0000-0000-0000-000000000000/resourcegroups/my-rg",
		"time": "2020-01-01T00:00:00.0000000Z",
		"data": {
			"authorization": {},
			"claims": {},
			"correlationId": "00000000-0000-0000-0000-000000000000",
			"httpRequest": {},
			"resourceProvider": "Microsoft.Resources",
			"resourceUri": "/subscriptions/00000000-0000-0000-0000-000000000000/resourcegroups/my-rg",
			"operationName": "Microsoft.Resources/subscriptions/resourceGroups/write",
			"status": "Canceled",
			"subscriptionId": "00000000-0000-0000-0000-000000000000",
			"tenantId": "00000000-0000-0000-0000-000000000000"
		}
	},
	{
		"id": "00000000-0000-0000-0000-000000000000",
		"source": "/subscriptions/00000000-0000-0000-0000-000000000000",
		"specversion": "1.0",
		"type": "Microsoft.Resources.ResourceWriteSuccess",
		"subject": "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/my-rg/providers/Microsoft.Compute/virtualMachines/my-vm",
		"time": "2020-01-01T00:00:00.0000000Z",
		"data": {
			"authorization": {},
			"claims": {},
			"correlationId": "00000000-0000-0000-0000-000000000000",
			"httpRequest": {},
			"resourceProvider": "Microsoft.Resources",
			"resourceUri": "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/my-rg/providers/Microsoft.Compute/virtualMachines/my-vm",
			"operationName": "Microsoft.Resources/tags/write",
			"status": "Succeeded",
			"subscriptionId": "00000000-0000-0000-0000-000000000000",
			"tenantId": "00000000-0000-0000-0000-000000000000"
		}
	},
	{
		"id": "00000000-0000-0000-0000-000000000000",
		"source": "/subscriptions/00000000-0000-0000-0000-000000000000",
		"specversion": "1.0",
		"type": "Microsoft.Resources.ResourceWriteSuccess",
		"subject": "/subscriptions/00000000-0000-0000-0000-000000000000/resourcegroups/my-rg/providers/Microsoft.Storage/storageAccounts/account",
		"time": "2020-01-01T00:00:00.0000000Z",
		"data": {
			"authorization": {},
			"claims": {},
			"correlationId": "00000000-0000-0000-0000-000000000000",
			"httpRequest": {},
			"resourceProvider": "Microsoft.Resources",
			"resourceUri": "/subscriptions/00000000-0000-0000-0000-000000000000/resourcegroups/my-rg/providers/Microsoft.Storage/storageAccounts/account",
			"operationName": "Microsoft.Resources/tags/write",
			"status": "Succeeded",
			"subscriptionId": "00000000-0000-0000-0000-000000000000",
			"tenantId": "00000000-0000-0000-0000-000000000000"
		}
	}]`)

	eventDataResourcesExpectedData = []source.Data{
		{
			Type:      "Microsoft.Resources/resourceGroups",
			Operation: source.DataOperationDelete,
			Time:      time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
			Values: map[string]any{
				// the source lowercases every id so that all the ingestion paths converge
				"id":   "/subscriptions/00000000-0000-0000-0000-000000000000/resourcegroups/my-rg",
				"type": "Microsoft.Resources/resourceGroups",
			},
		},
		{
			Type:      "Microsoft.Resources/resourceGroups",
			Operation: source.DataOperationUpsert,
			Time:      time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
			Values: map[string]any{
				// the source lowercases every id so that all the ingestion paths converge
				"id":   "/subscriptions/00000000-0000-0000-0000-000000000000/resourcegroups/my-rg",
				"type": "Microsoft.Resources/resourceGroups",
			},
		},
	}
)
