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

## Resource identifiers

Microsoft Azure does not guarantee the letter case of the resource IDs it returns: the same
resource can arrive with `resourceGroups` from the resource graph APIs and with `resourcegroups`
from the resource provider that answers the EventHub driven read, and the provider and type
segments vary in the same way. Because the mappings hash the ID to build the Catalog identifier,
every casing difference would create a duplicate item instead of updating the existing one.

To prevent that the source normalises every resource before handing it to the mapper:

- `id` is lowercased in full
- `type` is set to the resource type exactly as the mapping file declares it

The two values are therefore identical for `ibdm sync azure` and `ibdm run azure`, which makes
`{{ .id | sha256sum }}` a stable identifier and lets a delete event target the item a previous
import created.

This is also in part suggested by Azure, since it is stated that various APIs can return names with different casing,
therefore in order to perform meaningful matches a case-insensitive comparison is recommended.
For a more in-depth explanation refer to [Naming rules and restrictions for Azure resources].

### Consequences for the mappings and items

`id` is lowercase and could no longer match the casing shown in the Azure portal.
Use `.name` wherever available to display casing matters, its availability is dependant on the specific resource APIs.
The spelling Azure reported, if needed, is written to the source logs at the `Debug` level whenever it differs from the normalised value.

## Authentication

The source is using the [`DefaultAzureCredential` chain of authentication] so you can setup
your preferred method of login.  
This authentication will be used for reading data from the REST APIs so it will need the read
permissions on the resources you want to import.
Both `sync` and `run` modes use APIs to fetch the full resource, for this reason an authentication method of choice is always needed.

If you choose to don’t use the `*_CONNECTION_STING` variables the same authentication will be used
to receive data from the configured EventHub and to manage object inside the StorageAccount blob
storage.

[`DefaultAzureCredential` chain of authentication]: https://learn.microsoft.com/en-gb/azure/developer/go/sdk/authentication/credential-chains#defaultazurecredential-overview
[Naming rules and restrictions for Azure resources]: https://learn.microsoft.com/en-us/azure/azure-resource-manager/management/resource-name-rules
