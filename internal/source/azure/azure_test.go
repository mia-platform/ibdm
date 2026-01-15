// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package azure

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	fakeazcore "github.com/Azure/azure-sdk-for-go/sdk/azcore/fake"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resourcegraph/armresourcegraph"
	fakearmresourcegraph "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resourcegraph/armresourcegraph/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/source"
)

var (
	testTime = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
)

func init() {
	timeProvider = func() time.Time {
		return testTime
	}
}

func TestNewSource(t *testing.T) {
	testCases := map[string]struct {
		setupEnv       func(*testing.T)
		expectedConfig config
		expectedErr    error
	}{
		"no envs return no error": {
			setupEnv: func(t *testing.T) {
				t.Helper()
			},
			expectedConfig: config{
				EventHubConsumerGroup: "$Default",
			},
		},
		"all envs set": {
			setupEnv: func(t *testing.T) {
				t.Helper()
				t.Setenv("AZURE_SUBSCRIPTION_ID", "id")
				t.Setenv("AZURE_EVENT_HUB_CONNECTION_STRING", "eventhub-conn-string")
				t.Setenv("AZURE_EVENT_HUB_NAMESPACE", "namespace")
				t.Setenv("AZURE_EVENT_HUB_NAME", "name")
				t.Setenv("AZURE_EVENT_HUB_CONSUMER_GROUP", "group")
				t.Setenv("AZURE_STORAGE_BLOB_CONNECTION_STRING", "storage-conn-string")
				t.Setenv("AZURE_STORAGE_BLOB_ACCOUNT_NAME", "account-name")
				t.Setenv("AZURE_STORAGE_BLOB_CONTAINER_NAME", "container-name")
			},
			expectedConfig: config{
				SubscriptionID:             "id",
				EventHubConnectionString:   "eventhub-conn-string",
				EventHubNamespace:          "namespace",
				EventHubName:               "name",
				EventHubConsumerGroup:      "group",
				CheckpointConnectionString: "storage-conn-string",
				CheckpointStorageAccount:   "account-name",
				CheckpointContainerName:    "container-name",
			},
		},
	}

	for testName, test := range testCases {
		t.Run(testName, func(t *testing.T) {
			test.setupEnv(t)
			source, err := NewSource()
			if test.expectedErr != nil {
				assert.ErrorIs(t, err, test.expectedErr)
				assert.Nil(t, source)
				return
			}
			assert.NoError(t, err)
			assert.NotNil(t, source)
			assert.NotNil(t, source.azureCredentials)
			source.azureCredentials = nil // to be able to compare structs
			assert.Equal(t, test.expectedConfig, source.config)
		})
	}
}

func TestStartSyncProcess(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		typesToFilter map[string]source.Extra
		expectedData  []source.Data
		expectedErr   error
	}{
		"resources groups": {
			typesToFilter: map[string]source.Extra{
				"Microsoft.Resources/resourceGroups": nil,
			},
			expectedData: []source.Data{
				{
					Type:      "Microsoft.Resources/resourceGroups",
					Operation: source.DataOperationUpsert,
					Time:      testTime,
					Values: map[string]any{
						"extendedLocation": nil,
						"id":               "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/name",
						"identity":         nil,
						"kind":             "",
						"location":         "region",
						"managedBy":        "",
						"name":             "name",
						"plan":             nil,
						"properties": map[string]any{
							"provisioningState": "Succeeded",
						},
						"sku":  nil,
						"tags": map[string]any{},
						"type": "Microsoft.Resources/resourceGroups",
					},
				},
			},
		},
		"subscriptions": {
			typesToFilter: map[string]source.Extra{
				"Microsoft.Resources/subscriptions": nil,
			},
		},
		"multiple types": {
			typesToFilter: map[string]source.Extra{
				"Microsoft.Resources/resourceGroups": nil,
				"Microsoft.Compute/virtualMachines":  nil,
			},
			expectedData: []source.Data{
				{
					Type:      "Microsoft.Resources/resourceGroups",
					Operation: source.DataOperationUpsert,
					Time:      testTime,
					Values: map[string]any{
						"extendedLocation": nil,
						"id":               "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/name",
						"identity":         nil,
						"kind":             "",
						"location":         "region",
						"managedBy":        "",
						"name":             "name",
						"plan":             nil,
						"properties": map[string]any{
							"provisioningState": "Succeeded",
						},
						"sku":  nil,
						"tags": map[string]any{},
						"type": "Microsoft.Resources/resourceGroups",
					},
				},
				{
					Type:      "Microsoft.Compute/virtualMachines",
					Operation: source.DataOperationUpsert,
					Time:      testTime,
					Values: map[string]any{
						"extendedLocation": nil,
						"id":               "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/name/providers/Microsoft.Compute/virtualMachines/vm-name",
						"identity":         nil,
						"kind":             "",
						"location":         "northeurope",
						"managedBy":        "",
						"name":             "vm-name",
						"plan":             nil,
						"properties": map[string]any{
							"additionalCapabilities": map[string]any{},
							"diagnosticsProfile":     map[string]any{},
							"extended":               map[string]any{},
							"hardwareProfile":        map[string]any{},
							"networkProfile":         map[string]any{},
							"osProfile":              map[string]any{},
							"provisioningState":      "Succeeded",
							"storageProfile":         map[string]any{},
							"timeCreated":            "2020-01-01T00:00:00.0000000Z",
							"vmId":                   "00000000-0000-0000-0000-000000000000",
						},
						"sku":  nil,
						"tags": nil,
						"type": "Microsoft.Compute/virtualMachines",
					},
				},
				{
					Type:      "Microsoft.Compute/virtualMachines",
					Operation: source.DataOperationUpsert,
					Time:      testTime,
					Values: map[string]any{
						"extendedLocation": nil,
						"id":               "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/name/providers/Microsoft.Compute/virtualMachines/vm-name2",
						"identity":         nil,
						"kind":             "",
						"location":         "northeurope",
						"managedBy":        "",
						"name":             "vm-name2",
						"plan":             nil,
						"properties": map[string]any{
							"additionalCapabilities": map[string]any{},
							"diagnosticsProfile":     map[string]any{},
							"extended":               map[string]any{},
							"hardwareProfile":        map[string]any{},
							"networkProfile":         map[string]any{},
							"osProfile":              map[string]any{},
							"provisioningState":      "Succeeded",
							"storageProfile":         map[string]any{},
							"timeCreated":            "2020-01-01T00:00:00.0000000Z",
							"vmId":                   "00000000-0000-0000-0000-000000000001",
						},
						"sku":  nil,
						"tags": nil,
						"type": "Microsoft.Compute/virtualMachines",
					},
				},
			},
		},
		"error during request": {
			typesToFilter: map[string]source.Extra{
				"Microsoft.Resources/errorResources": nil,
			},
			expectedErr: assert.AnError,
		},
	}

	for testName, test := range testCases {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
			defer cancel()

			azureSource := &Source{
				config: config{
					SubscriptionID: "00000000-0000-0000-0000-000000000000",
					clientOptions: &arm.ClientOptions{
						ClientOptions: policy.ClientOptions{
							Transport: fakeResourceGraphTransport(t),
						},
					},
					azureCredentials: &fakeazcore.TokenCredential{},
				},
			}

			dataChannel := make(chan source.Data)
			go func() {
				defer close(dataChannel)
				err := azureSource.StartSyncProcess(ctx, test.typesToFilter, dataChannel)
				if test.expectedErr != nil {
					assert.ErrorIs(t, err, test.expectedErr)
					return
				}
				assert.NoError(t, err)
			}()

			var receivedData []source.Data
		loop:
			for {
				select {
				case <-ctx.Done():
					t.Fatal("test timed out")
				case data, open := <-dataChannel:
					if !open {
						break loop
					}
					receivedData = append(receivedData, data)
				}
			}

			assert.Equal(t, test.expectedData, receivedData)
		})
	}
}

func TestCancelledSyncProcess(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel() // cancel immediately

	azureSource := &Source{
		config: config{
			SubscriptionID: "00000000-0000-0000-0000-000000000000",
			clientOptions: &arm.ClientOptions{
				ClientOptions: policy.ClientOptions{
					Transport: fakeResourceGraphTransport(t),
				},
			},
			azureCredentials: &fakeazcore.TokenCredential{},
		},
	}

	dataChannel := make(chan source.Data)
	err := azureSource.StartSyncProcess(ctx, map[string]source.Extra{
		"Microsoft.Resources/resourceGroups": nil,
	}, dataChannel)
	close(dataChannel)
	assert.NoError(t, err)
	assert.Empty(t, dataChannel)
}

func TestInvalidSyncConfig(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
	defer cancel()

	azureSource := &Source{
		config: config{},
	}

	err := azureSource.StartSyncProcess(ctx, nil, nil)
	assert.ErrorIs(t, err, ErrMissingEnvVariable)
}

func TestDoubleStartSyncProcess(t *testing.T) {
	t.Parallel()

	syncChannel := make(chan struct{})
	azureSource := &Source{
		config: config{
			SubscriptionID: "00000000-0000-0000-0000-000000000000",
			clientOptions: &arm.ClientOptions{
				ClientOptions: policy.ClientOptions{
					Transport: fakearmresourcegraph.NewServerTransport(&fakearmresourcegraph.Server{
						Resources: func(ctx context.Context, query armresourcegraph.QueryRequest, options *armresourcegraph.ClientResourcesOptions) (resp fakeazcore.Responder[armresourcegraph.ClientResourcesResponse], errResp fakeazcore.ErrorResponder) {
							close(syncChannel) // signal that the first sync has started
							<-ctx.Done()
							return resp, errResp
						},
					}),
				},
			},
			azureCredentials: &fakeazcore.TokenCredential{},
		},
	}

	dataChannel := make(chan source.Data)

	go func() {
		err := azureSource.StartSyncProcess(t.Context(), map[string]source.Extra{"test": nil}, dataChannel)
		assert.NoError(t, err)
		close(dataChannel)
	}()

	<-syncChannel
	err := azureSource.StartSyncProcess(t.Context(), map[string]source.Extra{}, dataChannel)
	assert.NoError(t, err)

	azureSource.Close(t.Context(), 1*time.Second)
	<-dataChannel
}

func TestStartEventStreamProcess(t *testing.T) {
	t.Parallel()
}

func fakeResourceGraphTransport(t *testing.T) policy.Transporter {
	t.Helper()
	return fakearmresourcegraph.NewServerTransport(&fakearmresourcegraph.Server{
		Resources: func(_ context.Context, query armresourcegraph.QueryRequest, _ *armresourcegraph.ClientResourcesOptions) (responder fakeazcore.Responder[armresourcegraph.ClientResourcesResponse], errResponder fakeazcore.ErrorResponder) {
			switch resp, err := handleResourceGraphQueryRequest(t, query); {
			case resp != nil:
				responder.SetResponse(http.StatusOK, *resp, nil)
			case err != nil:
				errResponder.SetError(err)
			}

			return responder, errResponder
		},
	})
}

func handleResourceGraphQueryRequest(t *testing.T, query armresourcegraph.QueryRequest) (*armresourcegraph.ClientResourcesResponse, error) {
	t.Helper()
	require.NotNil(t, query.Query)
	require.Equal(t, []*string{to.Ptr("00000000-0000-0000-0000-000000000000")}, query.Subscriptions)

	switch *query.Query {
	case fmt.Sprintf(resourceContainerGraphQueryTemplate, "Microsoft.Resources/subscriptions/resourceGroups"):
		return &armresourcegraph.ClientResourcesResponse{
			QueryResponse: armresourcegraph.QueryResponse{
				TotalRecords:    to.Ptr(int64(0)),
				Data:            resourceGraphResourceGroupsResponse,
				ResultTruncated: to.Ptr(armresourcegraph.ResultTruncatedFalse),
				Count:           to.Ptr(int64(0)),
				SkipToken:       nil,
			},
		}, nil
	case fmt.Sprintf(resourceContainerGraphQueryTemplate, "Microsoft.Resources/subscriptions"):
		return &armresourcegraph.ClientResourcesResponse{
			QueryResponse: armresourcegraph.QueryResponse{
				TotalRecords:    to.Ptr(int64(0)),
				Data:            nil,
				ResultTruncated: to.Ptr(armresourcegraph.ResultTruncatedFalse),
				Count:           to.Ptr(int64(0)),
				SkipToken:       nil,
			},
		}, nil
	case fmt.Sprintf(resourceGraphQueryTemplate, "Microsoft.Compute/virtualMachines"):
		if query.Options != nil && query.Options.SkipToken != nil && *query.Options.SkipToken == "skip-token-1" {
			return &armresourcegraph.ClientResourcesResponse{
				QueryResponse: armresourcegraph.QueryResponse{
					TotalRecords:    to.Ptr(int64(2)),
					Data:            resourceGraphVirtualMachinesResponseTwo,
					ResultTruncated: to.Ptr(armresourcegraph.ResultTruncatedFalse),
					Count:           to.Ptr(int64(1)),
					SkipToken:       nil,
				},
			}, nil
		}

		return &armresourcegraph.ClientResourcesResponse{
			QueryResponse: armresourcegraph.QueryResponse{
				TotalRecords:    to.Ptr(int64(2)),
				Data:            resourceGraphVirtualMachinesResponseOne,
				ResultTruncated: to.Ptr(armresourcegraph.ResultTruncatedTrue),
				Count:           to.Ptr(int64(1)),
				SkipToken:       to.Ptr("skip-token-1"),
			},
		}, nil
	case fmt.Sprintf(resourceGraphQueryTemplate, "Microsoft.Resources/errorResources"):
		return nil, assert.AnError
	}

	return nil, fmt.Errorf("unknown query: %s", *query.Query)
}

var (
	resourceGraphResourceGroupsResponse = []any{
		map[string]any{
			"extendedLocation": nil,
			"id":               "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/name",
			"identity":         nil,
			"kind":             "",
			"location":         "region",
			"managedBy":        "",
			"name":             "name",
			"plan":             nil,
			"properties": map[string]any{
				"provisioningState": "Succeeded",
			},
			"sku":  nil,
			"tags": map[string]any{},
			"type": "microsoft.resources/subscriptions/resourcegroups",
		},
	}

	resourceGraphVirtualMachinesResponseOne = []any{
		map[string]any{
			"extendedLocation": nil,
			"id":               "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/name/providers/Microsoft.Compute/virtualMachines/vm-name",
			"identity":         nil,
			"kind":             "",
			"location":         "northeurope",
			"managedBy":        "",
			"name":             "vm-name",
			"plan":             nil,
			"properties": map[string]any{
				"additionalCapabilities": map[string]any{},
				"diagnosticsProfile":     map[string]any{},
				"extended":               map[string]any{},
				"hardwareProfile":        map[string]any{},
				"networkProfile":         map[string]any{},
				"osProfile":              map[string]any{},
				"provisioningState":      "Succeeded",
				"storageProfile":         map[string]any{},
				"timeCreated":            "2020-01-01T00:00:00.0000000Z",
				"vmId":                   "00000000-0000-0000-0000-000000000000",
			},
			"sku":  nil,
			"tags": nil,
			"type": "microsoft.compute/virtualmachines",
		},
	}

	resourceGraphVirtualMachinesResponseTwo = []any{
		map[string]any{
			"extendedLocation": nil,
			"id":               "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/name/providers/Microsoft.Compute/virtualMachines/vm-name2",
			"identity":         nil,
			"kind":             "",
			"location":         "northeurope",
			"managedBy":        "",
			"name":             "vm-name2",
			"plan":             nil,
			"properties": map[string]any{
				"additionalCapabilities": map[string]any{},
				"diagnosticsProfile":     map[string]any{},
				"extended":               map[string]any{},
				"hardwareProfile":        map[string]any{},
				"networkProfile":         map[string]any{},
				"osProfile":              map[string]any{},
				"provisioningState":      "Succeeded",
				"storageProfile":         map[string]any{},
				"timeCreated":            "2020-01-01T00:00:00.0000000Z",
				"vmId":                   "00000000-0000-0000-0000-000000000001",
			},
			"sku":  nil,
			"tags": nil,
			"type": "microsoft.compute/virtualmachines",
		},
	}
)
