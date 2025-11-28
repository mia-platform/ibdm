# GCP Bucket mapping

This document describes the GCP Bucket mapping used to convert GCP inventory Pub/Sub events into a normalized asset event.

Purpose

- Normalize GCP Storage Bucket events emitted by the inventory Pub/Sub source.
- Prepare a compact asset object with a consistent shape for downstream processing or sinks.

Mapped fields

- id (PK): the ID of the bucket. For buckets, the id and name properties are the same
- name: the name of the bucket
- kind: the kind of resource being described. For buckets, this is always "storage#bucket"
- labels: user-provided bucket labels, in key-value pairs
- location: the location of the bucket. Object data for objects in the bucket resides in physical storage within this location
- locationType: the type of location that the bucket resides in. Possible values include region, dual-region, and multi-region
- storageClass: the bucket's default storage class, used whenever no storageClass is specified for a newly-created object. If storageClass is not specified when the bucket is created, it defaults to "STANDARD"
- timeCreated: The creation time of the bucket in RFC 3339 format
- updated: The time at which the bucket's metadata or IAM policy was last updated, in RFC 3339 format
- versioning: wether the versioning is enabled for the bucket
- hierarchicalNamespace: whether or not hierarchical namespace is enabled for this bucket

```json
{
	"type": "storage.googleapis.com/Bucket",
	"syncable": true,
	"apiVersion": "buckets.gcp.mia-platform.eu/v1alpha1",
	"mappings": {
		"identifier": "{{resource.data.id}}",
		"specs": {
			"name": "{{resource.data.name}}",
			"kind": "{{resource.data.kind}}",
			"labels": "{{resource.data.labels}}",
			"location": "{{resource.data.location}}",
			"locationType": "{{resource.data.locationType}}",
			"storageClass": "{{resource.data.storageClass}}",
			"timeCreated": "{{resource.data.timeCreated}}",
			"updated": "{{resource.data.updated}}",
			"versioning": {
				"enabled": "{{resource.data.versioning | toJSON}}"
			},
			"hierarchicalNamespace": {
				"enabled": "{{resource.data.hierarchicalNamespace | toJSON}}"
			}
		}
	}
}
```

## Example

```json
{
    "id": "custom-bucket-1470",
    "name": "custom-bucket-1470",
    "kind": "storage#bucket",
    "labels": {
        "custom": "1470"
    },
    "location": "US",
    "locationType": "multi-region",
    "storageClass": "STANDARD",
    "timeCreated": "2025-10-10T10:38:12.324Z",
    "updated": "2025-10-10T10:38:12.324Z",
    "versioning": {
			"enabled": false
		},
    "hierarchicalNamespace": {
			"enabled": false
		},
}
```
