# GCP Folder mapping

This document describes the GCP Folder mapping used to convert GCP Pub/Sub events into a normalized asset event.

Purpose

- Normalize GCP Folder events emitted by the Pub/Sub source.
- Prepare a compact asset object with a consistent shape for downstream processing or sinks.

Mapped fields

- name (PK?): the resource name of the folder, in the form `folders/{folder_id}`
- parent: required, the parent's resource identifier (for example, `organizations/{org_id}` or `folders/{folder_id}`)
- displayName: a user-visible name for the folder
- lifecycleState: the current lifecycle state of the folder. (Possible values include LIFECYCLE_STATE_UNSPECIFIED, ACTIVE, DELETE_REQUESTED)
- createTime: RFC 3339 timestamp for when the folder was created
- tags: key/value pairs of user-defined tags associated with the folder

```json
{
	"type": "cloudresourcemanager.googleapis.com/Folder",
	"syncable": true,
	"apiVersion": "folders.gcp.mia-platform.eu/v1alpha1",
	"mappings": {
		"identifier": "{resource.data.name}}",
		"specs": {
			"parent": "{{resource.data.parent}}",
			"displayName": "{{resource.data.displayName}}",
			"lifecycleState": "{{resource.data.lifecycleState}}",
			"createTime": "{{resource.data.createTime}}",
			"tags": "{{ .resource.data.tags | toJSON}}"
		}
	}
}
```

## Example

```json
{
	"type": "cloudresourcemanager.googleapis.com/Folder",
	"syncable": true,
	"apiVersion": "folders.gcp.mia-platform.eu/v1alpha1",
	"mappings": {
		"identifier": "folders/123456789",
		"specs": {
			"parent": "organizations/987654321",
			"displayName": "Engineering",
			"lifecycleState": "ACTIVE",
			"createTime": "2024-07-01T12:00:00Z",
			"tags": {
				"env": "production",
				"cost-center": "eng-team"
			}
		}
	}
}
```

## Note

The "name" field seems to be treated as unique even though is not an actual unique identifier, like a more robust uid for example.

## Technical note

The fields and the examples of this entity have been realized through this documentation of the GKE REST API [Folder](https://cloud.google.com/resource-manager/reference/rest/v2/folders).
