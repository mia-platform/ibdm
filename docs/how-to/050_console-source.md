# Mia-Platform Console Integration

The Mia-Platform Console Integration of `ibdm` can work in two modes:

- receiving webhooks from the Console to react to events (e.g. project creation)
- syncing data via the Console APIs (projects, configurations)

## Commands

Once you have the `ibdm` binary available, running the integration is straightforward.

If you want to start listening for webhooks:

```sh
ibdm run console --mapping-file <path to mapping file or folder>
```

If you want to start a sync process to fetch data from the Console APIs:

```sh
ibdm sync console --mapping-file <path to mapping file or folder>
```

## Configuration

### Server

When running in webhook mode (`ibdm run console`), a server is spawned and these configs are supported:

- `HTTP_HOST`: The host of the underlying server
- `HTTP_PORT`: The port of the underlying server

### Webhook Mode

When running in webhook mode (`ibdm run console`), the following environment variables are supported:

- `CONSOLE_WEBHOOK_PATH`: The path where the webhook server will listen (default: `/console/webhook`).
- `CONSOLE_WEBHOOK_SECRET`: The secret shared with the Console to validate the `X-Mia-Signature` header.

### Sync Mode

When running in sync mode (`ibdm sync console`), the integration requires access to the Console APIs.

**General Configuration:**

- `CONSOLE_ENDPOINT`: The base API URL of the Mia-Platform Console (required). This must include the API path prefix, e.g. `https://console.example.com/api`.

**Authentication:**

The source supports Client Credentials (Client ID/Secret) authentication.

#### Client Credentials

To use Client Credentials, set the following environment variables:

- `CONSOLE_CLIENT_ID`: The Client ID.
- `CONSOLE_CLIENT_SECRET`: The Client Secret.
- `CONSOLE_AUTH_ENDPOINT`: (Optional) The token endpoint (default: `<CONSOLE_ENDPOINT>/m2m/oauth/token`).

## Data Types

The Console source can sync the following data types:

- `project`: Information about Console projects.
- `revision`: A named revision of a project.
- `service`: A microservice within a project's default-branch revision (only services of type `custom` and not marked as `advanced` are emitted).
- `cluster`: A cluster registered in the Console. Fetched via the tenant/cluster APIs; the `linkedProjects` field is stripped before mapping.
- `clusterProjectRelationship`: A relationship between a cluster and a linked project. One entry is emitted per project in the cluster's `linkedProjects` list, carrying both the project and the cluster (without `linkedProjects`) as template values.
