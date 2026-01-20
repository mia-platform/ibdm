// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package azure

import (
	"context"
	"encoding/json"
	"errors"
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
			assert.ElementsMatch(t, test.expectedData, receivedData)
		})
	}
}

func TestSendCloseChannel(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	client, err := armresources.NewClient("00000000-0000-0000-0000-000000000000", &fakeazcore.TokenCredential{}, &arm.ClientOptions{
		ClientOptions: policy.ClientOptions{
			Transport: fakearmresources.NewServerTransport(&fakearmresources.Server{
				GetByID: func(_ context.Context, resourceID, apiVersion string, _ *armresources.ClientGetByIDOptions) (responder fakeazcore.Responder[armresources.ClientGetByIDResponse], errResponder fakeazcore.ErrorResponder) {
					return responder, errResponder
				},
			}),
		},
	})
	require.NoError(t, err)

	_, err = client.GetByID(ctx, "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/myRG", "2021-04-01", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected error: %v", err)
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
	case "subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/myRG":
		assert.Equal(tb, "2021-04-01", apiVersion)
		resp = &armresources.ClientGetByIDResponse{
			GenericResource: armresources.GenericResource{
				ID:   to.Ptr("/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/myRG"),
				Type: to.Ptr("Microsoft.Resources/resourceGroups"),
			},
		}
		return resp, nil
	case "subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/myRG/providers/Microsoft.Compute/virtualMachines/myVM":
		assert.Equal(tb, "2021-07-01", apiVersion)
		return nil, assert.AnError
	}

	return nil, nil
}

var (
	eventDataResourcesTestBody = json.RawMessage(`[
	{
		"id": "00000000-0000-0000-0000-000000000000",
		"source": "/subscriptions/00000000-0000-0000-0000-000000000000",
		"specversion": "1.0",
		"type": "Microsoft.Resources.ResourceDeleteSuccess",
		"subject": "/subscriptions/00000000-0000-0000-0000-000000000000/resourcegroups/myRG",
		"time": "2020-01-01T00:00:00.0000000Z",
		"data": {
			"authorization": {},
			"claims": {},
			"correlationId": "00000000-0000-0000-0000-000000000000",
			"httpRequest": {},
			"resourceProvider": "Microsoft.Resources",
			"resourceUri": "/subscriptions/00000000-0000-0000-0000-000000000000/resourcegroups/myRG",
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
		"subject": "/subscriptions/00000000-0000-0000-0000-000000000000/resourcegroups/myRG",
		"time": "2020-01-01T00:00:00.0000000Z",
		"data": {
			"authorization": {},
			"claims": {},
			"correlationId": "00000000-0000-0000-0000-000000000000",
			"httpRequest": {},
			"resourceProvider": "Microsoft.Resources",
			"resourceUri": "/subscriptions/00000000-0000-0000-0000-000000000000/resourcegroups/myRG",
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
		"subject": "/subscriptions/00000000-0000-0000-0000-000000000000/resourcegroups/myRG",
		"time": "2020-01-01T00:00:00.0000000Z",
		"data": {
			"authorization": {},
			"claims": {},
			"correlationId": "00000000-0000-0000-0000-000000000000",
			"httpRequest": {},
			"resourceProvider": "Microsoft.Resources",
			"resourceUri": "/subscriptions/00000000-0000-0000-0000-000000000000/resourcegroups/myRG",
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
		"subject": "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/myRG/providers/Microsoft.Compute/virtualMachines/myVM",
		"time": "2020-01-01T00:00:00.0000000Z",
		"data": {
			"authorization": {},
			"claims": {},
			"correlationId": "00000000-0000-0000-0000-000000000000",
			"httpRequest": {},
			"resourceProvider": "Microsoft.Resources",
			"resourceUri": "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/myRG/providers/Microsoft.Compute/virtualMachines/myVM",
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
		"subject": "/subscriptions/00000000-0000-0000-0000-000000000000/resourcegroups/myRG/providers/Microsoft.Storage/storageAccounts/account",
		"time": "2020-01-01T00:00:00.0000000Z",
		"data": {
			"authorization": {},
			"claims": {},
			"correlationId": "00000000-0000-0000-0000-000000000000",
			"httpRequest": {},
			"resourceProvider": "Microsoft.Resources",
			"resourceUri": "/subscriptions/00000000-0000-0000-0000-000000000000/resourcegroups/myRG/providers/Microsoft.Storage/storageAccounts/account",
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
				"id":   "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/myRG",
				"type": "Microsoft.Resources/resourceGroups",
			},
		},
		{
			Type:      "Microsoft.Resources/resourceGroups",
			Operation: source.DataOperationUpsert,
			Time:      time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
			Values: map[string]any{
				"id":   "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/myRG",
				"type": "Microsoft.Resources/resourceGroups",
			},
		},
	}
)
