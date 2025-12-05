# GCP Disk mapping

This document describes the GCP Disk mapping used to convert Pub/Sub events into a normalized asset event.

Purpose

- Normalize Persistent Disk resources emitted by the Pub/Sub source.
- Prepare a compact asset object with a consistent shape for downstream processing or sinks.

Mapped fields

- id (PK): the unique identifier for the disk resource
- name: the user-assigned name of the disk
- architecture: (when present) the CPU architecture the disk image or snapshot targets
- enableConfidentialCompute: whether the disk is configured for Confidential Compute
- physicalBlockSizeBytes: the physical block size of the persistent disk, in bytes
- sizeGb: size of the disk in gigabytes
- status: current status of the disk (for example, READY)
- updateTime: RFC 3339 timestamp for the last update to the disk resource
- location: the zone or region where the disk resides
- version: present in GCP message, needs investigation if it related to message of item

```json
{
	"type": "compute.googleapis.com/Disk",
	"syncable": true,
	"apiVersion": "disks.gcp.mia-platform.eu/v1alpha1",
	"mappings": {
		"identifier": "{{resource.data.id}}",
		"specs": {
			"name": "{{resource.data.name}}",
			"architecture": "{{resource.data.architecture}}",
			"enableConfidentialCompute": "{{resource.data.enableConfidentialCompute}}",
			"physicalBlockSizeBytes": "{{resource.data.physicalBlockSizeBytes}}",
			"sizeGb": "{{resource.data.sizeGb}}",
			"status": "{{resource.data.status}}",
			"location": "{{resource.location}}",
			"updateTime": "{{updateTime}}"
		}
	}
}
```

## Example

```json
{
	"type": "compute.googleapis.com/Disk",
	"syncable": true,
	"apiVersion": "disks.gcp.mia-platform.eu/v1alpha1",
	"mappings": {
		"identifier": "1234567890123456789",
		"specs": {
			"name": "my-disk-1",
			"architecture": "X86_64",
			"enableConfidentialCompute": false,
			"physicalBlockSizeBytes": 4096,
			"sizeGb": 100,
			"status": "READY",
			"updateTime": "2025-10-15T08:22:33Z",
			"location": "us-central1-a",
			"version": "v1"
		}
	}
}
```

## Note

The root level fields "location" and "version" in this case are taken from the assets of the GCP PubSub event message,
but still are related to the retrieved entity, we can choose to use or not use depending on how they actually performs
in real scenarios with real data.
