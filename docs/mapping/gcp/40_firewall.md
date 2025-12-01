# GCP Firewall mapping

This document describes the GCP Firewall mapping used to convert GCP Pub/Sub events into a normalized asset event.

Purpose

- Normalize GCP Firewall events emitted by the Pub/Sub source.
- Prepare a compact asset object with a consistent shape for downstream processing or sinks.

Mapped fields

- id (PK): the unique identifier for the firewall
- name: the user-assigned name of the firewall
- description: a human-readable description of the firewall
- direction: the direction of traffic this firewall applies to (INGRESS or EGRESS)
- disabled: whether the firewall is disabled
- network: the network this firewall applies to
- allowed: protocol and ports that are allowed by this firewall
- targetTags: list of instance tags the firewall rules applies to. If specified, the rule applies only to instances in the VPC network that have one of those tags, otherwise to all instances
- updateTime: RFC 3339 timestamp for the last update to the firewall
- location: location context when applicable

```json
{
	"type": "compute.googleapis.com/Firewall",
	"syncable": true,
	"apiVersion": "firewalls.gcp.mia-platform.eu/v1alpha1",
	"mappings": {
		"identifier": "{{resource.data.id}}",
		"specs": {
			"id": "{{resource.data.id}}",
			"name": "{{resource.data.name}}",
			"description": "{{resource.data.description}}",
			"direction": "{{resource.data.direction}}",
			"disabled": "{{resource.data.disabled}}",
			"network": "{{resource.data.network}}",
			"allowed": "{{ .resource.data.allowed | toJSON}}",
			"targetTags": "{{resource.targetTags}}",
			"updateTime": "{{updateTime}}",
			"location": "{{location}}"
		}
	}
}
```

## Data Example JSON representation

```json
{
	"type": "compute.googleapis.com/Firewall",
	"syncable": true,
	"apiVersion": "disks.gcp.mia-platform.eu/v1alpha1",
	"mappings": {
		"identifier": "9876543210987654321",
		"specs": {
			"name": "allow-ssh-ingress",
			"description": "Allow SSH from internal network",
			"direction": "INGRESS",
			"disabled": false,
			"network": "projects/my-project/global/networks/default",
			"allowed": [
				{
					"IPProtocol": "tcp",
					"ports": [
						"22"
					]
				}
			],
			"targetTags": [
				"my-instance"
			],
			"updateTime": "2025-10-15T10:00:00Z",
			"location": "global"
		}
	}
}
```

## Note

The root level fields "location" in this case is taken from the assets of the GCP PubSub event message but still is related to the retrieved entity, we can choose to use or not use depending on how it actually performs in real scenarios with real data.
