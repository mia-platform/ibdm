# GCP Revision mapping

This document describes the GCP Revision mapping used to convert GCP Pub/Sub events into a normalized asset event.

Purpose

- Normalize GCP Folder events emitted by the Pub/Sub source.
- Prepare a compact asset object with a consistent shape for downstream processing or sinks.

Mapped fields

- apiVersion: the API version of the revision resource
- kind: the resource kind
- name: the name of the revision
- uid (PK): unique identifier for the revision
- labels: labels attached to the revision
- containers.name: name of the container in the revision
- containers.resources.limits.cpu: CPU limit for the container
- containers.resources.limits.memory: memory limit for the container
- updateTime: RFC 3339 timestamp for the last update to the revision
- location: the region where the revision is deployed

```json
{
  "type": "run.googleapis.com/Revision",
  "syncable": true,
  "apiVersion": "revisions.gcp.mia-platform.eu/v1alpha1",
  "mappings": {
    "identifier": "{{resource.data.uid}}",
    "specs": {
      "apiVersion": "{{resource.data.apiVersion}}",
      "kind": "{{resource.data.kind}}",
      "name": "{{resource.data.name}}",
      "labels": "{{ .resource.data.labels | toJSON}}",
      "containers": "{{ .resource.data.containers | toJSON}}",
      "updateTime": "{{updateTime}}",
      "location": "{{resource.location}}"
    }
  }
}
```

## Data Example JSON representation

```json
{
  "type": "run.googleapis.com/Revision",
  "syncable": true,
  "apiVersion": "revisions.gcp.mia-platform.eu/v1alpha1",
  "mappings": {
    "identifier": "36f4a1a1-b8e5-4eb9-9fad-3b4f177b9b11",
    "specs": {
      "apiVersion": "serving.knative.dev/v1",
      "kind": "Revision",
      "name": "worker-pool-00001-zpg",
      "labels": {
        "cloud.googleapis.com/location": "europe-west1",
        "run.googleapis.com/workerPool": "worker-pool",
        "run.googleapis.com/workerPoolUid": "ac58f5f0-97ec-4544-9e3f-dbbeace15b38"
      },
      "containers": [
        {
          "name": "worker-pool-1",
          "image": "us-docker.pkg.dev/cloudrun/container/worker-pool@sha256:490a8dc720df727947618b108f293810c8fc2de2cb8fb6af519318bbc5f51892",
          "resources": {
            "limits": {
              "cpu": "1",
              "memory": "512Mi"
            }
          }
        }
      ],
      "updateTime": "2025-10-13T13:07:01.803762Z",
      "location": "europe-west1"
    }
  }
}
```

## Related PubSub topic

- run.googleapis.com/Revision

## Note

The root level fields "location" and "version" in this case are taken from the assets of the GCP PubSub event message but still are related to the retrieved entity, we can choose to use or not use depending on how they actually performs in real scenarios with real data.
