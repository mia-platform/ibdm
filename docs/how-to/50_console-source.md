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

### Webhook Mode

When running in webhook mode (`ibdm run console`), the following environment variables are supported:

- `CONSOLE_WEBHOOK_PATH`: The path where the webhook server will listen (default: `/console-webhook`).
- `CONSOLE_WEBHOOK_SECRET`: The secret shared with the Console to validate the `X-Mia-Signature` header.

### Sync Mode

When running in sync mode (`ibdm sync console`), the integration requires access to the Console APIs.

**General Configuration:**

- `CONSOLE_ENDPOINT`: The base URL of the Mia-Platform Console (required).

**Authentication:**

The source supports two authentication methods relative to the Console: Client Credentials (Client ID/Secret) or Service Account (JWT).

#### Client Credentials

To use Client Credentials, set the following environment variables:

- `CONSOLE_CLIENT_ID`: The Client ID.
- `CONSOLE_CLIENT_SECRET`: The Client Secret.
- `CONSOLE_AUTH_ENDPOINT`: (Optional) The token endpoint (default: `<CONSOLE_ENDPOINT>/oauth/token`).

#### Service Account (JWT)

To use a Service Account with a private key, set the following environment variables:

- `CONSOLE_JWT_SERVICE_ACCOUNT`: Set to `true` to enable this mode.
- `CONSOLE_PRIVATE_KEY`: The private key for the service account.
- `CONSOLE_PRIVATE_KEY_ID`: The ID of the private key.

## Data Types

The Console source can sync the following data types:

- `project`: Information about Console projects.
- `configuration`: Configurations of the projects (fetched for each revision).
