# GCP Job mapping

This document describes the GCP Job mapping used to convert inventory Pub/Sub events into a normalized asset event.

Purpose

- Normalize Job resources (for example Cloud Run Jobs) emitted by the inventory Pub/Sub source.
- Prepare a compact asset object with a consistent shape for downstream processing or sinks.

Mapped fields

- apiVersion: the API version of the resource (for example, "run.googleapis.com/v1")
- kind: the resource kind (for example, "Job")
- name: the name of the Job resource
- labels: key/value labels attached to the Job
- uid (PK): unique identifier for the Job resource
- taskCount: optional, specifies the desired number of tasks the execution should run
- containers.name: name of the container defined in the Job
- containers.image: container image reference used by the Job
- containers.resources.limits.cpu: CPU resource limit for the container
- containers.resources.limits.memory: Memory resource limit for the container
- maxRetries: maximum number of retries for failed executions
- timeoutSeconds: maximum execution time in seconds before task termination
- discoveryName: internal discovery name (if present)
- location: location/region for the Job resource
- version: present in GCP message, needs investigation if it related to message of item

```json
{
  "type": "run.googleapis.com/Job",
  "syncable": true,
  "apiVersion": "jobs.gcp.mia-platform.eu/v1alpha1",
  "mappings": {
    "identifier": "{{resource.data.uid}}",
    "specs": {
      "apiVersion": "{{resource.data.apiVersion}}",
      "kind": "{{resource.data.kind}}",
      "name": "{{resource.data.name}}",
      "labels": "{{ .resource.data.labels | toJSON}}",
      "taskCount": "{{resource.data.taskCount}}",
      "containers": "{{ .resource.data.containers | toJSON}}",
      "maxRetries": "{{resource.data.maxRetries}}",
      "timeoutSeconds": "{{resource.data.timeoutSeconds}}",
      "discoveryName": "{{resource.data.discoveryName}}",
      "location": "{{resource.location}}"
    }
  }
}
```

## Example

```json
{
	"type": "run.googleapis.com/Job",
	"syncable": true,
	"apiVersion": "jobs.gcp.mia-platform.eu/v1alpha1",
	"mappings": {
		"identifier": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
		"specs": {
			"apiVersion": "run.googleapis.com/v1",
			"kind": "Job",
			"name": "greetings-job",
			"labels": {
				"team": "platform",
				"env": "staging"
			},
			"taskCount": 1,
			"containers": [
				{
					"name": "greeter",
					"image": "gcr.io/my-project/greeter:latest",
					"resources": {
						"limits": {
							"cpu": "0.5",
							"memory": "256Mi"
						}
					}
				}
			],
			"maxRetries": 3,
			"timeoutSeconds": 300,
			"discoveryName": "run.googleapis.com/Job",
			"location": "us-central1",
		}
	}
}
```

## Related PubSub topic

- run.googleapis.com/Job

## Note

The root level fields "location" and "version" in this case are taken from the assets of the GCP Pub/Sub event message but still are related to the retrieved entity, we can choose to use or not use depending on how they actually performs in real scenarios with real data.
