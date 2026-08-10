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

## Resource sub-types

Some Azure resource types describe more than one thing: a `Microsoft.Web/sites` resource is an App
Service site, but when its `kind` carries the `functionapp` token it is also a Function App.

For these types the Azure source produces, out of one Azure resource, both the item of the resource
itself and one or more **sub-type** items, each described by its own mapping file and related to the
item of the resource it was derived from.
A sub-type is always additive: the item of the Azure resource is produced exactly as it was before,
and the sub-type item is created next to it together with a relationship pointing at it.

### How a sub-type mapping is dispatched

The `type` of a sub-type mapping file is an **internal dispatch key**, not an Azure provider type:

- it is never sent to Azure and never used to build a resource graph query
- it is never matched against the resource type of an event, because only the source can decide to
	emit it
- its `extra.apiVersion` is never read, since the resource is always retrieved with the `apiVersion`
	of its parent type. It is kept in the file only for symmetry with every other Azure mapping

Which Azure type produces which sub-type is hardcoded in the source, together with the check deciding
whether a retrieved resource must produce it. Declaring a sub-type therefore takes both a new mapping
file and a change to that hardcoded dictionary: a sub-type can never be introduced by configuration
alone.

### The sub-types shipped with ibdm

| Azure type | Sub-type mapping | Produced when |
| --- | --- | --- |
| `Microsoft.Web/sites` | `docs/mappings/azure/websites_functionapps.yaml`, `type: functionapps` | the `kind` of the site carries the `functionapp` token |

`kind` is a comma separated list of tokens, such as `app`, `app,linux` or `functionapp,linux`, and
the tokens are compared one by one: a site whose kind is `myfunctionapp` is not a Function App.
A site without a usable `kind` produces no sub-type and nothing fails.

The mapping creates an `functionapps` item and, through its `extra` section, a `dependency`
relationship from that item to the `websites` item of the same site.

Both mapping files must be loaded for the sub-type to be produced. Loading
`docs/mappings/azure/websites.yaml` alone reproduces exactly the behaviour the source had before
sub-types existed, deletion included. Loading `websites_functionapps.yaml` alone can instead never
produce anything, so the source logs a warning when it starts and carries on.

`ibdm sync azure` and `ibdm run azure` behave identically, because the check runs on the payload the
Azure APIs returned and is indifferent to which of them retrieved it. To adopt a sub-type on an
already imported subscription load both mapping files and run `ibdm sync azure` once: every site that
already exists gets its sub-type item and its relationship.

### Deleting a resource that has sub-types

`Microsoft.Resources.ResourceDeleteSuccess` carries only the id of the deleted resource. Its `kind`
is gone and no API can return it any more, so at deletion time the check cannot run: the source
deletes the item of the resource **and the item of every sub-type its type can produce**, whether or
not that resource ever produced it.

For a `Microsoft.Web/sites` resource with both mapping files loaded, three deletions reach the
catalog:

| deleted | why |
| --- | --- |
| the `websites` item | the resource itself |
| the `functionapps` item | the only sub-type configured for its type |
| the relationship of the `functionapps` item | its `deletePolicy` is `cascade` |

A deletion addressed to a sub-type item the resource never produced is inert: the catalog publish
reports no per item outcome, so nothing fails and nothing is left behind. The identifier of a
sub-type item also lives in its own namespace, `functionapps-<resource id>` for the Function Apps,
so such a deletion can only ever name the sub-type item of that very resource.

Removing a sub-type mapping file is not the reverse operation: the items it already published stop
being updated and stop being deleted together with their resource, so they have to be removed by
hand.

### Authoring a sub-type mapping

- Declare `syncable: true`. Keeping a sub-type key out of the resource graph queries is the job of
	the hardcoded dictionary, not of `syncable`, and `syncable: false` would only risk confining the
	sub-type to `ibdm run azure`
- Build the identifier of the item, and the identifier of every `deletePolicy: "cascade"` extra, out
	of `.id` alone. A deletion payload carries only `id` and `type`, so a template reading any other
	field fails to render and that deletion is lost
- Give the sub-type item its own item family, so that its identifiers can never collide with the ones
	of another mapping, and make sure the item type definition for that family exists in the catalog
- Treat the payload as read only. A sub-type receives a shallow copy of the payload of its parent, so
	writing into a nested value, such as `properties` or `tags`, would be seen by every other item
	produced out of the same resource

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
