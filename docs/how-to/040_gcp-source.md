# Google Cloud Platform Integration

The Google Cloud Platform Integration of `ibdm` can work in two modes:

- subscribing to a PubSub that is receiving events from a GCP Cloud Asset Feed
- getting resources via the Cloud Asset REST APIs

## Commands

The commands below use every internal mapping shipped for the integration. To choose which of
them run, or to add your own mappings, see
[Internal and External Mappings](./015_internal-and-external-mappings.md).

Once you have the `ibdm` binary available the run of the integration is straightforward.

If you want to start a new integration with the PubSub subscription yuo can run the following
command:

```sh
ibdm run gcp --include-internal-mappings=all
```

if you want to start a Cloud Asset sync process run this instead:

```sh
ibdm sync gcp --include-internal-mappings=all
```

## Internal Mappings and Their Dependencies

`ibdm mappings list gcp` prints the internal mappings of this integration. Select them with
`--include-internal-mappings`, as described in
[Internal and External Mappings](./015_internal-and-external-mappings.md).

Every internal mapping is selected on its own. The sync asks the Cloud Asset API for exactly the asset types
of the selected mappings, in one request, and the Pub/Sub stream keeps only the events of those asset
types.

Some mappings also create relationships to the items of other mappings, described in
[Relationships](#relationships). The Catalog accepts a relationship even when the item at its other end does
not exist, so select all the mappings of a relationship to be able to navigate it.

## Relationships

The mappings below create `relationships` items next to their own item. Each relationship is created with the
item it is declared in and is deleted together with it (`deletePolicy: cascade`).

| Mapping | Relationship | Type | Built from |
| --- | --- | --- | --- |
| `clusters` | `Cluster` → `Network` | `dependency` | `networkConfig.network` of the cluster |
| `routers` | `Router` → `Network` | `part-of` | `network` of the router |
| `addresses` | `Router` → `Address` | `dependency` | the router in the `users` of the address |
| `addresses` | `ComputeInstance` → `Address` | `dependency` | the instance in the `users` of the address |
| `disks` | `Disk` → `ComputeInstance` | `dependency` | the instance in the `users` of the disk |
| `snapshots` | `Snapshot` → `Disk` | `dependency` | `sourceDisk` of the snapshot |
| `dnsrecordsets` | `DnsRecordSet` → `DnsZone` | `part-of` | the zone in the asset name of the record set |

### Finding the outbound NAT IPs of a cluster

The outbound IPs of a GKE cluster are the ones of the Cloud NAT gateways of its VPC network, in the region of the
cluster. Select the `clusters`, `routers` and `addresses` mappings together with `networks`, then navigate:

1. from the `Cluster` to its `Network`;
2. from the `Network` to the `Router` items that are part of it, and keep the ones whose `spec.location` is the
   region of the cluster (`spec.location` of the cluster);
3. from each `Router` to the `Address` items it depends on: they are the NAT IPs, whether they were reserved by
   you (`MANUAL_ONLY` NAT) or allocated by Google Cloud (`AUTO_ONLY` NAT, `spec.purpose` is `nat_auto`).

`spec.nats` of the router tells which NAT gateways it hosts, how they allocate IPs and which subnetworks they
translate.

### DNS records

The `dnsrecordsets` mapping brings every record set of every Cloud DNS managed zone, `NS` and `SOA` included, and
relates it to its zone. The mapping writes two items for each record set, the record set and its relationship, and
a zone can hold hundreds of record sets, so expect a large number of items when you select it. A record set is
identified by a hash of its asset name, because DNS names can hold characters an identifier cannot, and shown with
its DNS name.

### Snapshots, disks and virtual machines

Select the `snapshots`, `disks` and `computeinstances` mappings together to go from a snapshot to the virtual
machine that owns the disk it was taken from: a `Snapshot` depends on its `Disk`, which depends on the
`ComputeInstance` using it. A disk that is not attached to any instance has no relationship to a virtual machine,
and neither has a snapshot whose source disk was deleted or is not a disk of a project in scope. Snapshots made by
a snapshot schedule are included, with `spec.autoCreated` set, and can be numerous.

### Limitations

- The path goes through the VPC network, not through the exact cluster: a network with routers in several
  regions returns all of them, and the region of the cluster has to be used to select the right ones. A NAT that
  translates only some subnetworks (`LIST_OF_SUBNETWORKS`) is not related to the clusters of the other subnetworks.
- When the address has several routers or several instances in its `users`, or the disk several instances, only the first of each kind is related.
- A network of a Shared VPC is referenced with the project the network belongs to, which can differ from the one
  of the cluster.
- A relationship is deleted only when the item it is declared in is deleted, which `ibdm run` receives as a
  deletion event. `ibdm sync` never deletes, and a change that detaches an address from a router, without deleting
  the address, leaves the previous relationship in place.

## Configuration

In addition to other environment variables the GCP source can require additional ones:

- `GOOGLE_CLOUD_PUBSUB_PROJECT`: the project where the PubSub is located
- `GOOGLE_CLOUD_PUBSUB_SUBSCRIPTION`: the name of the subscription where the source will connect
- `GOOGLE_CLOUD_SYNC_PARENT`: name of the organization, folder, or project where the resources to
	sync are located. Must be one of `organizations/[organization-number]`, `projects/[project-id]`,
	`projects/[project-number]`, or `folders/[folder-number]`.

The `GOOGLE_CLOUD_PUBSUB_PROJECT` and `GOOGLE_CLOUD_PUBSUB_SUBSCRIPTION` are needed if you want
to get date trough PubSub, and only `GOOGLE_CLOUD_SYNC_PARENT` is needed for getting data
via the Cloud Asset API.

## Authentication

The GCP source support authentication through the [Application Default Credentials] or via
a service account key file located at the path set in the `GOOGLE_APPLICATION_CREDENTIALS`
env variable.

[Application Default Credentials]: https://docs.cloud.google.com/docs/authentication/application-default-credentials
