# GCP Service mapping

This document describes the GCP Service mapping used to convert Pub/Sub events into a normalized asset event.

Purpose

- Normalize service resources (e.g., Cloud Run Service, Endpoints) emitted by the Pub/Sub source.
- Prepare a compact asset object with a consistent shape for downstream processing or sinks.

Mapped fields

- apiVersion: the API version of the resource
- kind: the resource kind
- name: the name of the Service
- uid (PK): unique identifier for the Service
- labels: labels attached to the Service
- containerConcurrency: the maximum number of requests a single container instance can receive concurrently
- containers.name: container name defined in the service template
- containers.image: container image used by the service
- containers.ports.containerPort: the container port exposed by the container
- containers.resources.limits.cpu: CPU limit for the container
- containers.resources.limits.memory: memory limit for the container
- traffic: percentage of traffic routed to a revision or the latest revision
- updateTime: RFC 3339 timestamp for the last update to the service
- location: the region where the service is deployed

```json
{
  "type": "run.googleapis.com/Service",
  "syncable": true,
  "apiVersion": "services.gcp.mia-platform.eu/v1alpha1",
	"identifier": "{{resource.data.uid}}",
  "mappings": {
    "apiVersion": "{{resource.data.apiVersion}}",
    "kind": "{{resource.data.kind}}",
    "name": "{{resource.data.name}}",
		"labels": "{{ .resource.data.labels | toJSON}}",
    "containerConcurrency": "{{resource.data.containerConcurrency}}",
    "containers": "{{ .resource.data.containers | toJSON}}",
    "traffic": "{{ .resource.data.traffic | toJSON}}",
    "updateTime": "{{resource.data.updateTime}}",
    "location": "{{location}}"
  }
}
```

## Data Example JSON representation

```json
{
  "apiVersion": "serving.knative.dev/v1",
  "kind": "Service",
  "name": "hello-2",
  "uid": "8fe202c7-5658-4798-9f15-7f60b039f498",
  "labels": {
    "cloud.googleapis.com/location": "europe-west1"
  },
  "containerConcurrency": 80,
  "containers": [
    {
      "name": "hello-1",
      "image": "us-docker.pkg.dev/cloudrun/container/hello",
      "ports": [
        {
          "containerPort": 8080,
          "name": "http1"
        }
      ],
      "resources": {
        "limits": {
          "cpu": "1000m",
          "memory": "512Mi"
        }
      }
    }
  ],
  "traffic": [
    {
      "latestRevision": true,
      "percent": 100
    }
  ],
  "updateTime": "2025-10-13T09:53:13.6174Z",
  "location": "europe-west1",
}
```
