# Google Cloud Platform Integration

The Google Cloud Platform Integration of `ibdm` can work in two modes:

- subscribing to a PubSub that is receiving events from a GCP Cloud Asset Feed
- getting resources via the Cloud Asset REST APIs

## Commands

Once you have the `ibdm` binary available the run of the integration is straightforward.

If you want to start a new integration with the PubSub subscription yuo can run the following
command:

```sh
ibdm run gcp --mapping-file <path to mapping file or folder>
```

if you want to start a Cloud Asset sync process run this instead:

```sh
ibdm sync gcp --mapping-file <path to mapping file or folder>
```

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
