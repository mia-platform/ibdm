# GCP Project mapping

This document describes the GCP Project mapping used to convert GCP Pub/Sub events into a normalized asset event.

Purpose

- Normalize GCP Folder events emitted by the Pub/Sub source.
- Prepare a compact asset object with a consistent shape for downstream processing or sinks.

Mapped fields

- projectNumber (PK): the number uniquely identifying the project
- name: the resource name of the project (for example, `projects/{project_number}`)
- parent: the parent resource identifier (for example, `folders/{folder_id}` or `organizations/{org_id}`)
- projectId: the user-assigned project ID
- displayName: a user-visible name for the project
- state: the current lifecycle state of the project. (Possible values can be STATE_UNSPECIFIED, ACTIVE, DELETE_REQUESTED)
- createTime: RFC 3339 timestamp for when the project was created
- updateTime: RFC 3339 timestamp for the last update to the project
- deleteTime: RFC 3339 timestamp for when the project was deleted (if applicable)
- labels: key/value labels attached to the project
- tags: key/value tags associated with the project

```json
{
	"type": "cloudresourcemanager.googleapis.com/Project",
  "syncable": true,
  "apiVersion": "projects.gcp.mia-platform.eu/v1alpha1",
  "mappings": {
    "identifier": "{{resource.data.projectNumber}}",
    "specs": {
      "name": "{{resource.data.name}}",
      "parent": "{{resource.data.parent}}",
      "projectId": "{{resource.data.projectId}}",
      "displayName": "{{resource.data.displayName}}",
      "state": "{{resource.data.state}}",
      "createTime": "{{resource.data.createTime}}",
      "updateTime": "{{resource.data.updateTime}}",
      "labels": "{{ .resource.data.labels | toJSON}}",
      "tags": "{{ .resource.data.tags | toJSON}}"
    }
	}
}
```

## Data Example JSON representation

```json
{
	"type": "cloudresourcemanager.googleapis.com/Project",
  "syncable": true,
  "apiVersion": "projects.gcp.mia-platform.eu/v1alpha1",
	"identifier": "1234567890",
  "mappings": {
   "identifier": "1234567890",
    "specs": {
      "name": "projects/20318464073",
      "parent": "folders/918177713511",
      "projectId": "console-infrastructure-lab",
      "displayName": "Console Infrastructure Lab",
      "state": "ACTIVE",
      "createTime": "2023-01-15T09:30:00Z",
      "updateTime": "2025-09-01T12:00:00Z",
      "labels": {
        "env": "staging",
        "team": "platform"
      },
      "tags": {
        "cost-center": "infra"
      }
    }
  }
}
```
