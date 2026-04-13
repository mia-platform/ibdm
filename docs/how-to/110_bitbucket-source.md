# Bitbucket Source

The Bitbucket source connects IBDM to [Bitbucket Cloud](https://bitbucket.org/) to synchronise repository and
pipeline data into the Mia-Platform Catalog. It supports both pull-based sync (REST API) and real-time webhook events.

## Commands

### Sync

```bash
ibdm sync bitbucket --mapping-file <path to mapping file or folder>
```

Performs a one-off synchronisation: fetches all configured data from the Bitbucket REST API and exits.

### Run (Webhook Listener)

```bash
ibdm run bitbucket --mapping-file <path to mapping file or folder>
```

Starts a long-running HTTP server that listens for inbound Bitbucket webhook events.

## Configuration

All configuration is read from environment variables.

### Authentication

The Bitbucket source supports two **mutually exclusive** authentication modes. Exactly one must be configured.

#### Bearer Token

Set `BITBUCKET_ACCESS_TOKEN` to a Bitbucket access token (workspace, project, or repository scope).
Sent as `Authorization: Bearer <token>`.

#### Basic Auth

Set both `BITBUCKET_API_USERNAME` and `BITBUCKET_API_TOKEN`. The username is the Bitbucket username;
the token is the app password or repository/project access token. Sent as HTTP Basic Authentication.

Setting both modes simultaneously is a configuration error.

### Environment Variables

| Env Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `BITBUCKET_ACCESS_TOKEN` | One auth mode | _(empty)_ | Bitbucket access token for Bearer auth. |
| `BITBUCKET_API_USERNAME` | One auth mode | _(empty)_ | Username for HTTP Basic Authentication. |
| `BITBUCKET_API_TOKEN` | One auth mode | _(empty)_ | App password / access token for Basic Auth. |
| `BITBUCKET_URL` | No | `https://api.bitbucket.org` | Base URL of the Bitbucket API. |
| `BITBUCKET_HTTP_TIMEOUT` | No | `30s` | Timeout for HTTP requests. |
| `BITBUCKET_WORKSPACE` | No | _(empty)_ | Restrict sync to a single workspace slug. |
| `BITBUCKET_WEBHOOK_SECRET` | Webhook mode | _(empty)_ | HMAC secret for webhook signature validation. |
| `BITBUCKET_WEBHOOK_PATH` | No | `/bitbucket/webhook` | HTTP path for incoming webhook events. |

## Supported Data Types

| Type | Sync | Webhook |
| --- | --- | --- |
| `repository` | ✅ | ✅ |
| `pipeline` | ✅ | ❌ |

## Webhook Events

| Event Key | Produces |
| --- | --- |
| `repo:push` | `repository` upsert |
| `repo:updated` | `repository` upsert |
| `pullrequest:fulfilled` | `repository` upsert |

## Workspace Filtering

When `BITBUCKET_WORKSPACE` is set, the sync process is restricted to that single workspace.
When empty, IBDM calls `GET /2.0/user/workspaces` to enumerate all accessible workspaces automatically.
Setting this variable also means the token does not need workspace-listing permissions.
