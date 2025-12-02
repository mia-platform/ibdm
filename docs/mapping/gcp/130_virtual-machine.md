# GCP Virtual Machine mapping

This document describes the Compute Engine VM mapping used to convert Pub/Sub events into a normalized asset event.

Purpose

- Normalize VM instance resources emitted by the Pub/Sub source.
- Prepare a compact asset object with a consistent shape for downstream processing or sinks.

Mapped fields

- id (PK): the unique identifier for the instance
- name: instance name
- description: human-readable description
- cpuPlatform: CPU platform string
- deletionProtection: whether deletion protection is enabled
- disks: attached disk details including architecture, autoDelete, boot, deviceName, diskSizeGb, interface, mode, and type
- labels: labels attached to the instance (probably not user-defined in this case)
- status: current status of the instance (for example, RUNNING, PROVISIONING, STAGING)
- tags: network tags and fingerprint
- updateTime: RFC 3339 timestamp for the last update
- location: the region where the instance is deployed

```json
{
  "type": "compute.googleapis.com/Instance",
  "syncable": true,
  "apiVersion": "virtualmachines.compute.gcp.mia-platform.eu/v1alpha1",
  "mappings": {
    "identifier": "{{resource.data.id}}",
    "specs": {
      "name": "{{resource.data.name}}",
      "description": "{{resource.data.description}}",
      "cpuPlatform": "{{resource.data.cpuPlatform}}",
      "deletionProtection": "{{resource.data.deletionProtection}}",
      "disks": "{{ .resource.data.disks | toJSON}}",
      "labels": "{{ .resource.data.labels | toJSON}}",
      "status": "{{resource.data.status}}",
      "tags": "{{ .resource.data.tags | toJSON}}",
      "updateTime": "{{updateTime}}",
      "location": "{{resource.location}}"
    }
  }
}
```

## Data Example JSON representation

```json
{
  "type": "compute.googleapis.com/Instance",
  "syncable": true,
  "apiVersion": "virtualmachines.compute.gcp.mia-platform.eu/v1alpha1",
  "mappings": {
    "identifier": "9002296853278048281",
    "specs": {
      "name": "instance-20251013-132607",
      "description": "",
      "cpuPlatform": "Unknown CPU Platform",
      "deletionProtection": false,
      "disks": [
        {
          "architecture": "X86_64",
          "autoDelete": true,
          "boot": true,
          "deviceName": "instance-20251013-132607",
          "diskSizeGb": "10",
          "interface": "SCSI",
          "mode": "READ_WRITE",
          "type": "PERSISTENT"
        }
      ],
      "labels": {
        "goog-ops-agent-policy": "v2-x86-template-1-4-0"
      },
      "status": "STAGING",
      "tags": {
        "fingerprint": "42WmSpB8rSM="
      },
      "updateTime": "2025-10-13T13:30:35.594183Z",
      "location": "europe-west1-d",
    }
  }
}
```
