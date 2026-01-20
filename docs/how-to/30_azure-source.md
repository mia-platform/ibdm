# Microsoft Azure Integration

The Microsoft Azure Integration of `ibdm` can work in two modes:

- subscribing to Microsoft Azure subscription events through EventHub
- getting resources via the resource graph APIs

## Commands

Once you have the `ibdm` binary available the run of the integration is straightforward.

If you want to start a new integration with the EventHub subscription yuo can run the following
command:

```sh
ibdm run azure --mapping-file <path to mapping file or folder>
```

if you want to start a resource graph sync process run this instead:

```sh
ibdm sync azure --mapping-file <path to mapping file or folder>
```

## Configurations

In addition to other environment variables the Microsoft Azure source can require additional ones:

- `AZURE_SUBSCRIPTION_ID`: the Microsoft Azure subscription id that the source will connect to
- `AZURE_EVENT_HUB_CONNECTION_STRING`: the connection string for connecting to the Azure EventHub
	that will relay the subscription system events
- `AZURE_EVENT_HUB_NAMESPACE`: the name or fully qualified host of the Azure EventHub namespace
	that will relay the subscription system events
- `AZURE_EVENT_HUB_NAME`: the name of the Azure EventHub that will relay the subscription system
	events
- `AZURE_EVENT_HUB_CONSUMER_GROUP`: an optional consumer group name, by default `$Default` will be
	used
- `AZURE_STORAGE_BLOB_CONNECTION_STRING`: the connection string to an Azure StorageAccount with a
	blob container that will be used as an EventHub checkpoint storage
- `AZURE_STORAGE_BLOB_ACCOUNT_NAME`: the name of an Azure StorageAccount with a blob container
	that will be used as an EventHub checkpoint storage
- `AZURE_STORAGE_BLOB_CONTAINER_NAME`: the name of the blob container inside the Azure
	StorageAccount

For both the modes `AZURE_SUBSCRIPTION_ID` is required and it will be used for all the API calls
to the REST APIs of Microsoft Azure.  
All the other variables are needed only if you want to connect to Azure EventHub.

If you use the `AZURE_EVENT_HUB_CONNECTION_STRING` you will not need to set
`AZURE_EVENT_HUB_NAMESPACE` and `AZURE_EVENT_HUB_NAME` and, if you use the
`AZURE_STORAGE_BLOB_CONNECTION_STRING` you will not need to set the
`AZURE_STORAGE_BLOB_ACCOUNT_NAME` and `AZURE_STORAGE_BLOB_CONTAINER_NAME`.

Using the `*_CONNECTION_STRING` variables is the preferred methods that will also allow you to
setup the least privileges to the service account responsible to retrieve information from the
REST APIs.

## Authentication

The source is using the [`DefaultAzureCredential` chain of authentication] so you can setup
your preferred method of login.  
This authentication will be used for reading data from the REST APIs so it will need the read
permissions on the resources you want to import.

If you choose to don’t use the `*_CONNECTION_STING` variables the same authentication will be used
to receive data from the configured EventHub and to manage object inside the StorageAccount blob
storage.

[`DefaultAzureCredential` chain of authentication]: https://learn.microsoft.com/en-gb/azure/developer/go/sdk/authentication/credential-chains#defaultazurecredential-overview
