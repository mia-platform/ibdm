// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package azure

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigValidateForSync(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		cfg         config
		expectedErr error
	}{
		"missing subscription id": {
			cfg:         config{},
			expectedErr: ErrMissingEnvVariable,
		},
		"valid": {
			cfg: config{SubscriptionID: "sub"},
		},
	}

	for testName, test := range testCases {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			err := test.cfg.validateForSync()
			if test.expectedErr != nil {
				assert.ErrorIs(t, err, test.expectedErr)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestConfigValidateForEventStream(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		cfg         config
		expectedErr error
	}{
		"missing subscription id": {
			cfg:         config{},
			expectedErr: ErrMissingEnvVariable,
		},
		"missing event hub connection string and namespace": {
			cfg: config{
				SubscriptionID: "sub",
			},
			expectedErr: ErrInvalidEnvVariable,
		},
		"namespace present but missing event hub name": {
			cfg: config{
				SubscriptionID:           "sub",
				EventHubNamespace:        "ns",
				CheckpointStorageAccount: "account",
				CheckpointContainerName:  "container",
			},
			expectedErr: ErrMissingEnvVariable,
		},
		"missing checkpoint connection string and storage account": {
			cfg: config{
				SubscriptionID:        "sub",
				EventHubNamespace:     "ns",
				EventHubName:          "hub",
				EventHubConsumerGroup: "$Default",
			},
			expectedErr: ErrInvalidEnvVariable,
		},
		"storage account present but missing container name": {
			cfg: config{
				SubscriptionID:           "sub",
				EventHubNamespace:        "ns",
				EventHubName:             "hub",
				EventHubConsumerGroup:    "$Default",
				CheckpointStorageAccount: "account",
			},
			expectedErr: ErrMissingEnvVariable,
		},
		"valid": {
			cfg: config{
				SubscriptionID:             "sub",
				EventHubNamespace:          "ns",
				EventHubName:               "hub",
				EventHubConsumerGroup:      "$Default",
				CheckpointStorageAccount:   "account",
				CheckpointContainerName:    "container",
				CheckpointConnectionString: "",
			},
		},
		"valid with event hub connection string": {
			cfg: config{
				SubscriptionID:           "sub",
				EventHubConnectionString: "Endpoint=sb://ns.servicebus.windows.net/;SharedAccessKeyName=name;SharedAccessKey=key",
				EventHubName:             "hub",
				EventHubConsumerGroup:    "$Default",
				CheckpointStorageAccount: "account",
				CheckpointContainerName:  "container",
			},
		},
		"valid with checkpoint connection string": {
			cfg: config{
				SubscriptionID:             "sub",
				EventHubNamespace:          "ns",
				EventHubName:               "hub",
				EventHubConsumerGroup:      "$Default",
				CheckpointConnectionString: "DefaultEndpointsProtocol=https;AccountName=account;AccountKey=key;EndpointSuffix=core.windows.net",
				CheckpointContainerName:    "container",
			},
		},
	}

	for testName, test := range testCases {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			err := test.cfg.validateForEventStream()
			if test.expectedErr != nil {
				assert.ErrorIs(t, err, test.expectedErr)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestConfigCheckpointServiceURL(t *testing.T) {
	testCases := map[string]struct {
		storageAccount string
		expectedURL    string
	}{
		"account name": {
			storageAccount: "storage-account",
			expectedURL:    "https://storage-account.blob.core.windows.net/",
		},
		"already qualified url": {
			storageAccount: "https://storage-account.blob.core.windows.net/",
			expectedURL:    "https://storage-account.blob.core.windows.net/",
		},
		"already qualified host": {
			storageAccount: "storage-account.blob.core.windows.net",
			expectedURL:    "storage-account.blob.core.windows.net",
		},
	}

	for testName, test := range testCases {
		t.Run(testName, func(t *testing.T) {
			cfg := config{CheckpointStorageAccount: test.storageAccount}
			assert.Equal(t, test.expectedURL, cfg.checkpointServiceURL())
		})
	}
}

func TestConfigEventHubFullyQualifiedNamespace(t *testing.T) {
	testCases := map[string]struct {
		namespace string
		expected  string
	}{
		"namespace": {
			namespace: "namespace",
			expected:  "namespace.servicebus.windows.net",
		},
		"already qualified": {
			namespace: "namespace.servicebus.windows.net",
			expected:  "namespace.servicebus.windows.net",
		},
	}

	for testName, test := range testCases {
		t.Run(testName, func(t *testing.T) {
			cfg := config{EventHubNamespace: test.namespace}
			assert.Equal(t, test.expected, cfg.eventHubFullyQualifiedNamespace())
		})
	}
}

func TestSetupEventStreamProcessors(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		cfg                 config
		expectedErrorString string
	}{
		"valid with connection strings": {
			cfg: config{
				EventHubConnectionString:   "Endpoint=sb://namespace.servicebus.windows.net/;SharedAccessKeyName=key-name;SharedAccessKey=a2V5;EntityPath=name",
				CheckpointConnectionString: "DefaultEndpointsProtocol=https;AccountName=account-name;AccountKey=a2V5;EndpointSuffix=core.windows.net",
				CheckpointContainerName:    "container",
			},
		},
		"valid with checkpoint single values": {
			cfg: config{
				EventHubConnectionString: "Endpoint=sb://namespace.servicebus.windows.net/;SharedAccessKeyName=key-name;SharedAccessKey=a2V5;EntityPath=hub-name",
				CheckpointStorageAccount: "account",
				CheckpointContainerName:  "container",
			},
		},
		"valid with event hub single values": {
			cfg: config{
				EventHubNamespace:          "namespace",
				EventHubName:               "name",
				CheckpointConnectionString: "DefaultEndpointsProtocol=https;AccountName=account-name;AccountKey=a2V5;EndpointSuffix=core.windows.net",
				CheckpointContainerName:    "container",
			},
		},
		"valid with all single values": {
			cfg: config{
				EventHubNamespace:        "namespace",
				EventHubName:             "name",
				CheckpointStorageAccount: "account",
				CheckpointContainerName:  "container",
			},
		},
		"wrong hub info": {
			cfg: config{
				EventHubConnectionString:   "Endpoint=sb://namespace.servicebus.windows.net/;SharedAccessKeyName=key-name;SharedAccessKey=a2V5;",
				CheckpointConnectionString: "DefaultEndpointsProtocol=https;AccountName=account-name;AccountKey=a2V5;EndpointSuffix=core.windows.net",
			},
			expectedErrorString: "eventHub cannot be an empty string",
		},
		"wrong checkpoint info": {
			cfg: config{
				EventHubConnectionString:   "Endpoint=sb://namespace.servicebus.windows.net/;SharedAccessKeyName=key-name;SharedAccessKey=a2V5;EntityPath=hub-name",
				CheckpointConnectionString: "DefaultEndpointsProtocol=https;AccountKey=a2V5;EndpointSuffix=core.windows.net",
			},
			expectedErrorString: "connection string needs either AccountName or BlobEndpoint",
		},
	}

	for testName, test := range testCases {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			client, processor, err := test.cfg.setupEventStreamProcessors()
			if len(test.expectedErrorString) > 0 {
				assert.ErrorContains(t, err, test.expectedErrorString)
				assert.Nil(t, client)
				assert.Nil(t, processor)
				return
			}
			defer client.Close(t.Context())

			require.NoError(t, err)
			require.NotNil(t, client)
			require.NotNil(t, processor)
		})
	}
}
