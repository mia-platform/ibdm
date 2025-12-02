# GCP Cluster mapping

This document describes the GCP Cluster mapping used to convert GCP Pub/Sub events into a normalized asset event.

Purpose

- Normalize GCP Cluster events emitted by the Pub/Sub source.
- Prepare a compact asset object with a consistent shape for downstream processing or sinks.

Mapped fields

- id (PK): cluster identifier
- name: cluster name
- description: human-readable description
- loggingService: configured logging service
- monitoringService: configured monitoring service
- network: network resource the cluster uses
- subnetwork: subnetwork resource the cluster uses
- addonsConfig: configuration for cluster add-ons
- nodePools: list of node pools with their name, locations, and version
- locations: list of cluster locations or zones
- resourceLabels: resource labels for the cluster to use to annotate any related Google Compute Engine resources
- networkPolicy: wether network policy is enabled
- maintenancePolicy: maintenance windows configuration
- autoscaling: autoprovisioning and resource limits settings
- networkConfig: network/subnetwork selection
- defaultMaxPodsConstraint: default max pods per node
- databaseEncryption: state of cluster database encryption
- verticalPodAutoscaling: enabled flag
- shieldedNodes: enabled flag
- currentMasterVersion: control plane version
- status: cluster status (PROVISIONING, RUNNING, etc.)
- location: primary location
- autopilot.enabled: whether Autopilot mode is enabled
- podAutoscaling.hpaProfile: HPA profile settings

```json
{
	"type": "storage.googleapis.com/Bucket",
	"syncable": true,
	"apiVersion": "buckets.gcp.mia-platform.eu/v1alpha1",
	"identifier": "{{resource.data.id}}",
	"mappings": {
		"name": "{{resource.data.name}}",
		"description": "{{resource.data.description}}",
		"loggingService": "{{resource.data.loggingService}}",
		"monitoringService": "{{resource.data.monitoringService}}",
		"network": "{{resource.data.network}}",
		"subnetwork": "{{resource.data.subnetwork}}",
		"addonsConfig": "{{ .resource.data.addonsConfig | toJSON}}",
		"nodePools": "{{ .resource.data.nodePools | toJSON}}",
		"locations": "{{ .resource.data.locations | toJSON}}",
		"resourceLabels": "{{ .resource.data.resourceLabels | toJSON}}",
		"networkPolicy": "{{ .resource.data.networkPolicy | toJSON}}",
		"maintenancePolicy": "{{ .resource.data.maintenancePolicy | toJSON}}",
		"autoscaling": "{{ .resource.data.autoscaling | toJSON}}",
		"defaultMaxPodsConstraint": "{{resource.data.defaultMaxPodsConstraint.maxPodsPerNode}}",
		"databaseEncryption": "{{resource.data.databaseEncryption.state}}",
		"verticalPodAutoscaling": "{{resource.data.verticalPodAutoscaling.enabled}}",
		"shieldedNodes": "{{resource.data.shieldedNodes.enabled}}",
		"currentMasterVersion": "1.33.4-gke.1350000",
		"status": "RUNNING",
		"location": "europe-west1",
		"autopilot": "{{resource.data.autopilot.enabled}}",
		"podAutoscaling": "{{resource.data.podAutoscaling.hpaProfile}}",
	}
}
```

## Data Example JSON representation

```json
{
  "type": "container.googleapis.com/Cluster",
  "syncable": true,
  "apiVersion": "clusters.gcp.mia-platform.eu/v1alpha1",
  "mappings": {
    "id": "autopilot-cluster-1-id",
    "name": "autopilot-cluster-1",
    "description": "Autopilot cluster for test",
    "loggingService": "logging.googleapis.com/kubernetes",
    "monitoringService": "monitoring.googleapis.com/kubernetes",
    "network": "projects/my-project/global/networks/default",
    "subnetwork": "projects/my-project/regions/us-central1/subnetworks/default",
    "addonsConfig": {
      "horizontalPodAutoscaling": {
        "disabled": false
      },
      "networkPolicyConfig": {
        "disabled": false
      },
      "cloudRunConfig": {
        "disabled": false,
        "loadBalancerType": "LOAD_BALANCER_TYPE_EXTERNAL"
      },
      "rayOperatorConfig": {
        "enabled": false
      }
    },
    "nodePools": [
      {
        "name": "default-pool",
        "locations": [
          "europe-west1-b"
        ],
        "version": "1.33.4-gke.1350000"
      }
    ],
    "locations": [
      "europe-west1"
    ],
    "resourceLabels": {
      "env": "staging"
    },
    "networkPolicy": {
      "provider": "CALICO",
      "enabled": true
    },
    "maintenancePolicy": {
      "dailyMaintenanceWindow": {
        "startTime": "03:00",
        "duration": "PT1H"
      }
    },
    "autoscaling": {
      "enableNodeAutoprovisioning": true,
      "autoscalingProfile": "OPTIMIZE_UTILIZATION",
      "autoprovisioningLocations": [
        "europe-west1"
      ]
    },
    "defaultMaxPodsConstraint": {
      "maxPodsPerNode": "110"
    },
    "databaseEncryption": {
      "state": "ENCRYPTED"
    },
    "verticalPodAutoscaling": {
      "enabled": true
    },
    "shieldedNodes": {
      "enabled": true
    },
    "currentMasterVersion": "1.33.4-gke.1350000",
    "status": "RUNNING",
    "location": "europe-west1",
    "autopilot": {
      "enabled": true
    },
    "podAutoscaling": {
      "hpaProfile": "PERFORMANCE"
    }
  }
}
```

## Related PubSub topic

- container.googleapis.com/Cluster

## Technical note

The fields and the examples of this entity have been realized through this documentation of the GKE REST API [Cluster](https://cloud.google.com/kubernetes-engine/docs/reference/rest/v1/projects.locations.clusters) since for technical reasons it was not possibile to have an actual subscription event message of this type. This kind of documentations for the other entities proved to be faithful to the structure of the entity received through the PubSub event messages.
