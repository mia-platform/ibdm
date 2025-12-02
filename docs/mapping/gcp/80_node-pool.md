# GCP Node Pool mapping

This document describes the GKE Node Pool mapping used to convert Pub/Sub events into a normalized asset event.

Purpose

- Normalize Node Pool resources emitted by the Pub/Sub source.
- Prepare a compact asset object with a consistent shape for downstream processing or sinks.

Mapped fields

- name (PK?): the name of the node pool
- locations: list of zones/locations where nodes are provisioned
- config.diskSizeGb: disk size for nodes, in GB
- config.diskType: disk type (for example, "pd-standard", "pd-balanced")
- autoscaling.enabled: whether autoscaling is enabled for the node pool
- autoscaling.locationPolicy: autoscaler location policy (for example, BALANCED)
- autoscaling.maxNodeCount: maximum number of nodes for autoscaling
- management.autoRepair: whether node auto-repair is enabled
- management.autoUpgrade: whether node auto-upgrade is enabled
- maxPodsConstraint.maxPodsPerNode: maximum pods allowed per node
- status: current status of the node pool (for example, RUNNING)
- version: Kubernetes version running on the nodes
- updateTime: RFC 3339 timestamp for last update
- location: location/region of the node pool

```json
{
  "type": "container.googleapis.com/NodePool",
  "syncable": true,
  "apiVersion": "nodepools.gcp.mia-platform.eu/v1alpha1",
  "mappings": {
    "identifier": "{{name}}",
    "specs": {
      "locations": "{ .resource.data.locations | toJSON}}",
      "config": "{{ .resource.data.config | toJSON}}",
      "autoscaling": "{{ .resource.data.autoscaling | toJSON}}",
      "management": "{{ .resource.data.management | toJSON}}",
      "maxPodsConstraint": "{{ .resource.data.maxPodsConstraint | toJSON}}",
      "status": "{{resource.data.status}}",
      "version": "{{resource.data.version}}",
      "updateTime": "{{resource.data.updateTime}}",
      "location": "{{resource.data.location}}"
    }
  }
}
```

## Data Example JSON representation

```json
{
  "type": "container.googleapis.com/NodePool",
  "syncable": true,
  "apiVersion": "nodepools.gcp.mia-platform.eu/v1alpha1",
  "mappings": {
    "identifier": "pool-6",
    "specs": {
      "locations": [
        "europe-west1-c",
        "europe-west1-d",
        "europe-west1-b"
      ],
      "config": {
        "diskSizeGb": 100,
        "diskType": "pd-balanced"
      },
      "autoscaling": {
        "enabled": true,
        "locationPolicy": "BALANCED",
        "maxNodeCount": 1000
      },
      "management": {
        "autoRepair": true,
        "autoUpgrade": true
      },
      "maxPodsConstraint": {
        "maxPodsPerNode": "32"
      },
      "status": "RUNNING",
      "version": "1.33.4-gke.1350000",
      "updateTime": "2025-10-14T13:40:49.899657Z",
      "location": "europe-west1"
    }
  }
}
```
