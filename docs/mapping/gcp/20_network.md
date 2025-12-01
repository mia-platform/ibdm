# GCP Network mapping

This document describes the GCP Network mapping used to convert GCP Pub/Sub events into a normalized asset event.

Purpose

- Normalize GCP Network events emitted by the Pub/Sub source.
- Prepare a compact asset object with a consistent shape for downstream processing or sinks.

Mapped fields

- id (PK): the unique identifier for the network resource
- name: the name of the network
- description: a human-readable description of the network
- mtu: the maximum transmission unit, in bytes
- routingConfig.routingMode: the routing mode for the network (for example, REGIONAL or GLOBAL)
- updateTime: RFC 3339 timestamp for the last update to the network resource
- location: location context when applicable

```json
{
	"type": "compute.googleapis.com/Network",
	"syncable": true,
	"apiVersion": "networks.gcp.mia-platform.eu/v1alpha1",
	"mappings": {
		"identifier": "{{resource.data.id}}",
		"specs": {
			"name": "{{resource.data.name}}",
			"description": "{{resource.data.description}}",
			"mtu": "{{resource.data.mtu}}",
			"routingConfig": "{{ .resource.data.routingConfig.routingMode | toJSON}}",
			"location": "{{resource.location}}",
			"updateTime": "{{updateTime}}"
		}
	}
}
```

## Example

```json
{
	"type": "compute.googleapis.com/Network",
	"syncable": true,
	"apiVersion": "networks.gcp.mia-platform.eu/v1alpha1",
	"identifier": "447776895153723587",
	"mappings": {
		"name": "vpc-network-test",
		"description": "this is a test for a network",
		"mtu": 1460,
		"routingConfig": {
			"routingMode": "REGIONAL"
		},
		"updateTime": "2025-10-14T15:03:08.591868Z",
		"location": "global"
	}
}
```
