// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package azure

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azeventhubs/v2"
	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azeventhubs/v2/checkpoints"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resourcegraph/armresourcegraph"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources/v3"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
)

var (
	// ErrMissingEnvVariable reports missing mandatory environment variables.
	ErrMissingEnvVariable = errors.New("missing environment variable")
	// ErrInvalidEnvVariable reports malformed environment variable values.
	ErrInvalidEnvVariable = errors.New("invalid environment value")
)

// config holds all the configuration needed to connect to Azure.
type config struct {
	SubscriptionID string `env:"AZURE_SUBSCRIPTION_ID"`

	EventHubConnectionString string `env:"AZURE_EVENT_HUB_CONNECTION_STRING"`
	EventHubNamespace        string `env:"AZURE_EVENT_HUB_NAMESPACE"`
	EventHubName             string `env:"AZURE_EVENT_HUB_NAME"`
	EventHubConsumerGroup    string `env:"AZURE_EVENT_HUB_NAMESPACE" envDefault:"$Default"`

	CheckpointConnectionString string `env:"AZURE_STORAGE_BLOB_CONNECTION_STRING"`
	CheckpointStorageAccount   string `env:"AZURE_STORAGE_BLOB_ACCOUNT_NAME"`
	CheckpointContainerName    string `env:"AZURE_STORAGE_BLOB_CONTAINER_NAME"`
}

// validateForSync checks if the configuration is valid for sync operations.
func (c config) validateForSync() error {
	if len(c.SubscriptionID) == 0 {
		return fmt.Errorf("%w: %s", ErrMissingEnvVariable, "AZURE_SUBSCRIPTION_ID")
	}

	return nil
}

// validateForEventStream checks if the configuration is valid for event stream operations.
func (c config) validateForEventStream() error {
	switch {
	case len(c.SubscriptionID) == 0:
		return fmt.Errorf("%w: %s", ErrMissingEnvVariable, "AZURE_SUBSCRIPTION_ID")
	case len(c.EventHubConnectionString) == 0 && len(c.EventHubNamespace) == 0:
		return fmt.Errorf("%w: %s", ErrInvalidEnvVariable, "one of AZURE_EVENT_HUB_CONNECTION_STRING or AZURE_EVENT_HUB_NAMESPACE must be present")
	case len(c.EventHubNamespace) > 0 && len(c.EventHubName) == 0:
		return fmt.Errorf("%w: %s", ErrMissingEnvVariable, "AZURE_EVENT_HUB_NAME")
	case len(c.CheckpointConnectionString) == 0 && len(c.CheckpointStorageAccount) == 0:
		return fmt.Errorf("%w: %s", ErrInvalidEnvVariable, "one of AZURE_STORAGE_BLOB_CONNECTION_STRING or AZURE_STORAGE_BLOB_ACCOUNT_NAME must be present")
	case len(c.CheckpointStorageAccount) > 0 && len(c.CheckpointContainerName) == 0:
		return fmt.Errorf("%w: %s", ErrMissingEnvVariable, "AZURE_STORAGE_BLOB_CONTAINER_NAME")
	}

	return nil
}

func (c config) checkpointServiceURL() string {
	if strings.Contains(c.CheckpointStorageAccount, ".blob.core.windows.net") {
		return c.CheckpointStorageAccount
	}

	return fmt.Sprintf("https://%s.blob.core.windows.net/", c.CheckpointStorageAccount)
}

func (c config) eventHubFullyQualifiedNamespace() string {
	if strings.Contains(c.EventHubNamespace, ".servicebus.windows.net") {
		return c.EventHubNamespace
	}

	return c.EventHubNamespace + ".servicebus.windows.net"
}

func (c config) setupEventStreamProcessors() (*azeventhubs.ConsumerClient, *azeventhubs.Processor, error) {
	azureCredentials, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, nil, err
	}

	eventHubClient, err := c.newEventHubClient(azureCredentials)
	if err != nil {
		return nil, nil, err
	}

	checkpointClient, err := c.newCheckpointClient(azureCredentials)
	if err != nil {
		return nil, nil, err
	}

	processor, err := azeventhubs.NewProcessor(eventHubClient, checkpointClient, nil)
	if err != nil {
		return nil, nil, err
	}

	return eventHubClient, processor, nil
}

func (c config) newCheckpointClient(credentials azcore.TokenCredential) (azeventhubs.CheckpointStore, error) {
	var storageAccountClient *azblob.Client
	if c.CheckpointConnectionString != "" {
		client, err := azblob.NewClientFromConnectionString(c.CheckpointConnectionString, nil)
		if err != nil {
			return nil, err
		}
		storageAccountClient = client
	} else {
		client, err := azblob.NewClient(c.checkpointServiceURL(), credentials, nil)
		if err != nil {
			return nil, err
		}
		storageAccountClient = client
	}

	containerClient := storageAccountClient.ServiceClient().NewContainerClient(c.CheckpointContainerName)
	return checkpoints.NewBlobStore(containerClient, nil)
}

func (c config) newEventHubClient(credentials azcore.TokenCredential) (*azeventhubs.ConsumerClient, error) {
	if c.EventHubConnectionString != "" {
		return azeventhubs.NewConsumerClientFromConnectionString(
			c.EventHubConnectionString,
			c.EventHubName,
			c.EventHubConsumerGroup,
			nil,
		)
	}

	return azeventhubs.NewConsumerClient(
		c.eventHubFullyQualifiedNamespace(),
		c.EventHubName,
		c.EventHubConsumerGroup,
		credentials,
		nil,
	)
}

func (c config) azureGraphClient() (*armresourcegraph.Client, error) {
	azureCredentials, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, err
	}

	return armresourcegraph.NewClient(azureCredentials, nil)
}

func (c config) azureClient() (*armresources.Client, error) {
	azureCredentials, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, err
	}

	return armresources.NewClient(c.SubscriptionID, azureCredentials, nil)
}
