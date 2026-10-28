// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package azure

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
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

	"github.com/mia-platform/ibdm/internal/logger"
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
		typesToFilter map[string]source.MappingExtras
		azureData     *azeventhubs.ReceivedEventData
		expectedData  []source.Data
	}{
		"no events": {
			contextFunc: func(tb testing.TB) (context.Context, context.CancelFunc) {
				tb.Helper()
				return context.WithTimeout(tb.Context(), 1*time.Second)
			},
			typesToFilter: map[string]source.MappingExtras{
				"Microsoft.Resources/resourceGroups": {testMappingName: {"apiVersion": "2021-04-01"}},
				"Microsoft.Compute/virtualMachines":  {testMappingName: {"apiVersion": "2021-07-01"}},
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
			typesToFilter: map[string]source.MappingExtras{
				"Microsoft.Resources/resourceGroups": {testMappingName: {"apiVersion": "2021-04-01"}},
				"Microsoft.Compute/virtualMachines":  {testMappingName: {"apiVersion": "2021-07-01"}},
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
			typesToFilter: map[string]source.MappingExtras{
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
			typesToFilter: map[string]source.MappingExtras{
				"Microsoft.Resources/resourceGroups": {testMappingName: {"apiVersion": "2021-04-01"}},
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
			typesToFilter: map[string]source.MappingExtras{
				managedClustersType: {testMappingName: {"apiVersion": managedClustersAPIVersion}},
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
			typesToFilter: map[string]source.MappingExtras{
				managedClustersType: {testMappingName: {"apiVersion": managedClustersAPIVersion}},
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
		"a site carrying the functionapp kind emits its item and the one of its sub-type": {
			contextFunc: func(tb testing.TB) (context.Context, context.CancelFunc) {
				tb.Helper()
				return context.WithTimeout(tb.Context(), 1*time.Second)
			},
			typesToFilter: map[string]source.MappingExtras{
				websitesType:     {testMappingName: {apiVersionKey: websitesAPIVersion}},
				functionAppsType: {testMappingName: {apiVersionKey: websitesAPIVersion}},
			},
			azureData: &azeventhubs.ReceivedEventData{
				EventData: azeventhubs.EventData{
					Body: eventDataFunctionAppWriteBody,
				},
			},
			expectedData: []source.Data{
				{
					Type:      websitesType,
					Operation: source.DataOperationUpsert,
					Time:      time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
					Values:    streamedFunctionAppValues(),
				},
				{
					// the sub-type carries the payload of its parent, whose type stays the Azure
					// provider type: a sub-type key is a dispatch key and never reaches the item
					Type:      functionAppsType,
					Operation: source.DataOperationUpsert,
					Time:      time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
					Values:    streamedFunctionAppValues(),
				},
			},
		},
		"a site not carrying the functionapp kind emits its item alone": {
			contextFunc: func(tb testing.TB) (context.Context, context.CancelFunc) {
				tb.Helper()
				return context.WithTimeout(tb.Context(), 1*time.Second)
			},
			typesToFilter: map[string]source.MappingExtras{
				websitesType:     {testMappingName: {apiVersionKey: websitesAPIVersion}},
				functionAppsType: {testMappingName: {apiVersionKey: websitesAPIVersion}},
			},
			azureData: &azeventhubs.ReceivedEventData{
				EventData: azeventhubs.EventData{
					Body: eventDataWebsiteWriteBody,
				},
			},
			expectedData: []source.Data{
				{
					Type:      websitesType,
					Operation: source.DataOperationUpsert,
					Time:      time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
					Values: map[string]any{
						"id":   normalizedWebsiteID,
						"kind": webAppKindValue,
						"type": websitesType,
					},
				},
			},
		},
		"deleting a site broadcasts to every configured sub-type": {
			contextFunc: func(tb testing.TB) (context.Context, context.CancelFunc) {
				tb.Helper()
				return context.WithTimeout(tb.Context(), 1*time.Second)
			},
			typesToFilter: map[string]source.MappingExtras{
				websitesType:     {testMappingName: {apiVersionKey: websitesAPIVersion}},
				functionAppsType: {testMappingName: {apiVersionKey: websitesAPIVersion}},
			},
			azureData: &azeventhubs.ReceivedEventData{
				EventData: azeventhubs.EventData{
					Body: eventDataFunctionAppDeleteBody,
				},
			},
			// the event carries only the resource id, so the kind check cannot run and the sub-type
			// is deleted whether or not the site ever produced it
			expectedData: []source.Data{
				{
					Type:      websitesType,
					Operation: source.DataOperationDelete,
					Time:      time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
					Values: map[string]any{
						"id":   normalizedFunctionAppID,
						"type": websitesType,
					},
				},
				{
					Type:      functionAppsType,
					Operation: source.DataOperationDelete,
					Time:      time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
					Values: map[string]any{
						"id":   normalizedFunctionAppID,
						"type": websitesType,
					},
				},
			},
		},
		"deleting a site without the sub-type mapping loaded keeps the previous behaviour": {
			contextFunc: func(tb testing.TB) (context.Context, context.CancelFunc) {
				tb.Helper()
				return context.WithTimeout(tb.Context(), 1*time.Second)
			},
			typesToFilter: map[string]source.MappingExtras{
				websitesType: {testMappingName: {apiVersionKey: websitesAPIVersion}},
			},
			azureData: &azeventhubs.ReceivedEventData{
				EventData: azeventhubs.EventData{
					Body: eventDataFunctionAppDeleteBody,
				},
			},
			expectedData: []source.Data{
				{
					Type:      websitesType,
					Operation: source.DataOperationDelete,
					Time:      time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
					Values: map[string]any{
						"id":   normalizedFunctionAppID,
						"type": websitesType,
					},
				},
			},
		},
		"a sub-type mapping loaded without its parent produces nothing": {
			contextFunc: func(tb testing.TB) (context.Context, context.CancelFunc) {
				tb.Helper()
				return context.WithTimeout(tb.Context(), 1*time.Second)
			},
			typesToFilter: map[string]source.MappingExtras{
				functionAppsType: {testMappingName: {apiVersionKey: websitesAPIVersion}},
			},
			azureData: &azeventhubs.ReceivedEventData{
				EventData: azeventhubs.EventData{
					Body: eventDataFunctionAppWriteBody,
				},
			},
		},
		"delete with non canonical subject casing": {
			contextFunc: func(tb testing.TB) (context.Context, context.CancelFunc) {
				tb.Helper()
				return context.WithTimeout(tb.Context(), 1*time.Second)
			},
			typesToFilter: map[string]source.MappingExtras{
				managedClustersType: {testMappingName: {"apiVersion": managedClustersAPIVersion}},
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
	case functionAppGetByIDPath:
		assert.Equal(tb, websitesAPIVersion, apiVersion)
		resp = &armresources.ClientGetByIDResponse{
			GenericResource: armresources.GenericResource{
				ID:   to.Ptr(azureFunctionAppID),
				Kind: to.Ptr(functionAppKindValue),
				Type: to.Ptr(websitesType),
			},
		}
		return resp, nil
	case websiteGetByIDPath:
		assert.Equal(tb, websitesAPIVersion, apiVersion)
		resp = &armresources.ClientGetByIDResponse{
			GenericResource: armresources.GenericResource{
				ID:   to.Ptr(azureWebsiteID),
				Kind: to.Ptr(webAppKindValue),
				Type: to.Ptr(websitesType),
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

	// the same keys for the two App Service sites: their subjects already spell the resource group
	// literal in camelCase, so the fake receives azureFunctionAppID and azureWebsiteID without the
	// leading slash.
	functionAppGetByIDPath = "subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/my-rg/providers/Microsoft.Web/sites/my-function"
	websiteGetByIDPath     = "subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/my-rg/providers/Microsoft.Web/sites/my-site"
)

// streamedFunctionAppValues returns the payload the event handler emits for the App Service site
// carrying the functionapp kind token. It is built on every call because a sub-type receives its
// own copy of the payload of its parent.
func streamedFunctionAppValues() map[string]any {
	return map[string]any{
		"id":   normalizedFunctionAppID,
		"kind": functionAppKindValue,
		"type": websitesType,
	}
}

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

	// eventDataFunctionAppWriteBody and eventDataWebsiteWriteBody import the two App Service sites,
	// only the first of which carries the functionapp kind token in the body the resource provider
	// answers with.
	eventDataFunctionAppWriteBody = json.RawMessage(`[
	{
		"id": "00000000-0000-0000-0000-000000000000",
		"source": "/subscriptions/00000000-0000-0000-0000-000000000000",
		"specversion": "1.0",
		"type": "Microsoft.Resources.ResourceWriteSuccess",
		"subject": "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/my-rg/providers/Microsoft.Web/sites/my-function",
		"time": "2020-01-01T00:00:00.0000000Z"
	}]`)

	eventDataWebsiteWriteBody = json.RawMessage(`[
	{
		"id": "00000000-0000-0000-0000-000000000000",
		"source": "/subscriptions/00000000-0000-0000-0000-000000000000",
		"specversion": "1.0",
		"type": "Microsoft.Resources.ResourceWriteSuccess",
		"subject": "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/my-rg/providers/Microsoft.Web/sites/my-site",
		"time": "2020-01-01T00:00:00.0000000Z"
	}]`)

	// eventDataFunctionAppDeleteBody carries only the resource id, as every delete event does.
	eventDataFunctionAppDeleteBody = json.RawMessage(`[
	{
		"id": "00000000-0000-0000-0000-000000000000",
		"source": "/subscriptions/00000000-0000-0000-0000-000000000000",
		"specversion": "1.0",
		"type": "Microsoft.Resources.ResourceDeleteSuccess",
		"subject": "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/my-rg/providers/Microsoft.Web/sites/my-function",
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

// failingAPIVersion is the api-version the recording transport answers with an error.
const failingAPIVersion = "2020-01-01"

// apiVersionRecorder is a fake Resources API that serves the my-function site with a tag holding
// the api-version it was fetched with, and records the api-version of every request.
type apiVersionRecorder struct {
	lock        sync.Mutex
	apiVersions []string
}

func (r *apiVersionRecorder) transport(tb testing.TB) policy.Transporter {
	tb.Helper()

	return fakearmresources.NewServerTransport(&fakearmresources.Server{
		GetByID: func(_ context.Context, resourceID, apiVersion string, _ *armresources.ClientGetByIDOptions) (responder fakeazcore.Responder[armresources.ClientGetByIDResponse], errResponder fakeazcore.ErrorResponder) {
			r.lock.Lock()
			r.apiVersions = append(r.apiVersions, apiVersion)
			r.lock.Unlock()

			assert.Equal(tb, functionAppGetByIDPath, resourceID)
			if apiVersion == failingAPIVersion {
				errResponder.SetError(assert.AnError)
				return responder, errResponder
			}

			responder.SetResponse(http.StatusOK, armresources.ClientGetByIDResponse{
				GenericResource: armresources.GenericResource{
					ID:   to.Ptr(azureFunctionAppID),
					Kind: to.Ptr(functionAppKindValue),
					Type: to.Ptr(websitesType),
					Tags: map[string]*string{"fetchedWith": to.Ptr(apiVersion)},
				},
			}, nil)
			return responder, errResponder
		},
	})
}

// functionAppFetchedWith is the payload of the my-function site fetched with apiVersion.
func functionAppFetchedWith(apiVersion string) map[string]any {
	values := streamedFunctionAppValues()
	values["tags"] = map[string]any{"fetchedWith": apiVersion}
	return values
}

func TestPartitionEventHandlerAPIVersionGroups(t *testing.T) {
	t.Parallel()

	const olderAPIVersion = "2024-04-01"
	eventTime := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	functionAppsMappings := source.MappingExtras{"my-functions": {apiVersionKey: websitesAPIVersion}}

	testCases := map[string]struct {
		websitesMappings    source.MappingExtras
		eventBody           json.RawMessage
		expectedAPIVersions []string
		expectedData        []source.Data
	}{
		"mappings on two api-versions fetch once per version and each fetch targets its group": {
			websitesMappings: source.MappingExtras{
				"b-sites": {apiVersionKey: websitesAPIVersion},
				"a-sites": {apiVersionKey: olderAPIVersion},
			},
			eventBody:           eventDataFunctionAppWriteBody,
			expectedAPIVersions: []string{olderAPIVersion, websitesAPIVersion},
			expectedData: []source.Data{
				{Type: websitesType, Operation: source.DataOperationUpsert, Time: eventTime, Values: functionAppFetchedWith(olderAPIVersion), Mappings: []string{"a-sites"}},
				// the sub-type is emitted once, with the first fetch, and reaches all its mappings
				{Type: functionAppsType, Operation: source.DataOperationUpsert, Time: eventTime, Values: functionAppFetchedWith(olderAPIVersion)},
				{Type: websitesType, Operation: source.DataOperationUpsert, Time: eventTime, Values: functionAppFetchedWith(websitesAPIVersion), Mappings: []string{"b-sites"}},
			},
		},
		"mappings sharing an api-version fetch once and stay untargeted": {
			websitesMappings: source.MappingExtras{
				"a-sites": {apiVersionKey: websitesAPIVersion},
				"b-sites": {apiVersionKey: websitesAPIVersion},
			},
			eventBody:           eventDataFunctionAppWriteBody,
			expectedAPIVersions: []string{websitesAPIVersion},
			expectedData: []source.Data{
				{Type: websitesType, Operation: source.DataOperationUpsert, Time: eventTime, Values: functionAppFetchedWith(websitesAPIVersion)},
				{Type: functionAppsType, Operation: source.DataOperationUpsert, Time: eventTime, Values: functionAppFetchedWith(websitesAPIVersion)},
			},
		},
		"a mapping without api-version is left out of the fetch it cannot make": {
			websitesMappings: source.MappingExtras{
				"a-sites": {apiVersionKey: websitesAPIVersion},
				"b-sites": {apiVersionKey: websitesAPIVersion},
				"c-sites": nil,
			},
			eventBody:           eventDataFunctionAppWriteBody,
			expectedAPIVersions: []string{websitesAPIVersion},
			expectedData: []source.Data{
				{Type: websitesType, Operation: source.DataOperationUpsert, Time: eventTime, Values: functionAppFetchedWith(websitesAPIVersion), Mappings: []string{"a-sites", "b-sites"}},
				{Type: functionAppsType, Operation: source.DataOperationUpsert, Time: eventTime, Values: functionAppFetchedWith(websitesAPIVersion)},
			},
		},
		"a failed fetch skips its group and the sub-type comes with the first successful fetch": {
			websitesMappings: source.MappingExtras{
				"a-sites": {apiVersionKey: failingAPIVersion},
				"b-sites": {apiVersionKey: websitesAPIVersion},
			},
			eventBody:           eventDataFunctionAppWriteBody,
			expectedAPIVersions: []string{failingAPIVersion, websitesAPIVersion},
			expectedData: []source.Data{
				{Type: websitesType, Operation: source.DataOperationUpsert, Time: eventTime, Values: functionAppFetchedWith(websitesAPIVersion), Mappings: []string{"b-sites"}},
				{Type: functionAppsType, Operation: source.DataOperationUpsert, Time: eventTime, Values: functionAppFetchedWith(websitesAPIVersion)},
			},
		},
		"a delete is emitted once to every mapping able to fetch the resource": {
			websitesMappings: source.MappingExtras{
				"a-sites": {apiVersionKey: olderAPIVersion},
				"b-sites": {apiVersionKey: websitesAPIVersion},
				"c-sites": nil,
			},
			eventBody: eventDataFunctionAppDeleteBody,
			expectedData: []source.Data{
				{Type: websitesType, Operation: source.DataOperationDelete, Time: eventTime, Values: map[string]any{"id": normalizedFunctionAppID, "type": websitesType}, Mappings: []string{"a-sites", "b-sites"}},
				{Type: functionAppsType, Operation: source.DataOperationDelete, Time: eventTime, Values: map[string]any{"id": normalizedFunctionAppID, "type": websitesType}},
			},
		},
		"a delete reaching every mapping stays untargeted": {
			websitesMappings: source.MappingExtras{
				"a-sites": {apiVersionKey: olderAPIVersion},
				"b-sites": {apiVersionKey: websitesAPIVersion},
			},
			eventBody: eventDataFunctionAppDeleteBody,
			expectedData: []source.Data{
				{Type: websitesType, Operation: source.DataOperationDelete, Time: eventTime, Values: map[string]any{"id": normalizedFunctionAppID, "type": websitesType}},
				{Type: functionAppsType, Operation: source.DataOperationDelete, Time: eventTime, Values: map[string]any{"id": normalizedFunctionAppID, "type": websitesType}},
			},
		},
	}

	for testName, test := range testCases {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
			defer cancel()

			recorder := &apiVersionRecorder{}
			client, err := armresources.NewClient("sub-id", &fakeazcore.TokenCredential{}, &arm.ClientOptions{
				ClientOptions: policy.ClientOptions{Transport: recorder.transport(t)},
			})
			require.NoError(t, err)

			typesToFilter := map[string]source.MappingExtras{
				websitesType:     test.websitesMappings,
				functionAppsType: functionAppsMappings,
			}
			dataChannel := make(chan source.Data, 100)
			partitionEventHandler(client, typesToFilter, dataChannel)(ctx, &azeventhubs.ReceivedEventData{
				EventData: azeventhubs.EventData{Body: test.eventBody},
			})
			close(dataChannel)

			var receivedData []source.Data
			for data := range dataChannel {
				receivedData = append(receivedData, data)
			}

			require.NotErrorIs(t, ctx.Err(), context.DeadlineExceeded)
			require.Equal(t, test.expectedAPIVersions, recorder.apiVersions)
			require.Equal(t, test.expectedData, receivedData)
		})
	}
}

func TestWarnUnusableAPIVersions(t *testing.T) {
	t.Parallel()

	logs := &bytes.Buffer{}
	warnUnusableAPIVersions(logger.NewLogger(logs), map[string]source.MappingExtras{
		websitesType: {
			"a-sites": {apiVersionKey: time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)},
			"b-sites": {apiVersionKey: ""},
			"c-sites": {apiVersionKey: websitesAPIVersion},
			"d-sites": nil,
		},
		// a sub-type is fetched with the api-version of its parent mappings, so its own is not checked
		functionAppsType: {"my-functions": {apiVersionKey: 20250301}},
	})

	output := logs.String()
	require.Equal(t, 2, strings.Count(output, "mapping apiVersion is not a non-empty string"))
	require.Contains(t, output, `"mapping":"a-sites"`)
	require.Contains(t, output, `"valueType":"time.Time"`)
	require.Contains(t, output, `"mapping":"b-sites"`)
	require.NotContains(t, output, "my-functions")
}
