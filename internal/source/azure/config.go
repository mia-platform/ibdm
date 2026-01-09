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

func (c config) newCheckpointClient() (azeventhubs.CheckpointStore, error) {
	var storageAccountClient *azblob.Client
	if c.CheckpointConnectionString != "" {
		client, err := azblob.NewClientFromConnectionString(c.CheckpointConnectionString, nil)
		if err != nil {
			return nil, err
		}
		storageAccountClient = client
	} else {
		credentials, err := azureCredentials()
		if err != nil {
			return nil, err
		}

		client, err := azblob.NewClient(c.checkpointServiceURL(), credentials, nil)
		if err != nil {
			return nil, err
		}
		storageAccountClient = client
	}

	containerClient := storageAccountClient.ServiceClient().NewContainerClient(c.CheckpointContainerName)
	return checkpoints.NewBlobStore(containerClient, nil)
}

func (c config) newEventHubClient() (*azeventhubs.ConsumerClient, error) {
	if c.EventHubConnectionString != "" {
		return azeventhubs.NewConsumerClientFromConnectionString(
			c.EventHubConnectionString,
			c.EventHubName,
			c.EventHubConsumerGroup,
			nil,
		)
	}

	credentials, err := azureCredentials()
	if err != nil {
		return nil, err
	}

	return azeventhubs.NewConsumerClient(
		c.eventHubFullyQualifiedNamespace(),
		c.EventHubName,
		c.EventHubConsumerGroup,
		credentials,
		nil,
	)
}

func (c config) azureClientFactory() (*armresources.ClientFactory, error) {
	credentials, err := azureCredentials()
	if err != nil {
		return nil, err
	}

	return armresources.NewClientFactory(c.SubscriptionID, credentials, nil)
}

// azureCredentials creates a chained token credential using environment, workload identity,
// and managed identity credentials.
func azureCredentials() (azcore.TokenCredential, error) {
	// any failing credential is skipped to allow fallback to the next one
	if envCredentials, err := azidentity.NewEnvironmentCredential(nil); err == nil {
		return envCredentials, nil
	}

	if workloadCredentials, err := azidentity.NewWorkloadIdentityCredential(nil); err == nil {
		return workloadCredentials, nil
	}

	if managedIdentityCredentials, err := azidentity.NewManagedIdentityCredential(nil); err == nil {
		return managedIdentityCredentials, nil
	}

	return nil, errors.New("unable to find a valid Microsoft Azure credentials")
}
