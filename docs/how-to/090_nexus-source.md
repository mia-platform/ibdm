# Sonatype Nexus Repository Manager Integration

The Sonatype Nexus Repository Manager Integration of `ibdm` connects to
[Nexus Repository Manager](https://help.sonatype.com/en/automation.html#rest-api) via its REST API.
It supports both pull-based sync and real-time webhook events.

## Commands

### Sync

```sh
ibdm sync nexus --mapping-file <path to mapping file or folder>
```

Performs a one-off synchronisation: fetches all configured data from the Nexus REST API and exits.

### Run (Webhook Listener)

```sh
ibdm run nexus --mapping-file <path to mapping file or folder>
```

Starts a long-running HTTP server that listens for inbound Nexus webhook events and streams
component changes to the Mia-Platform Catalog in real time.

## Configuration

All configuration is read from environment variables.

### Environment Variables

| Env Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `NEXUS_URL_SCHEMA` | Yes | _(empty)_ | URL scheme of the Nexus instance (e.g. `https`). |
| `NEXUS_URL_HOST` | Yes | _(empty)_ | Hostname (and optional port) of the Nexus instance (e.g. `nexus.example.com`). No scheme, no trailing slash. |
| `NEXUS_TOKEN_NAME` | Yes | _(empty)_ | Nexus user token name — first part of a [user token](https://help.sonatype.com/en/user-tokens.html) pair. Sent as the HTTP Basic Auth username. |
| `NEXUS_TOKEN_PASSCODE` | Yes | _(empty)_ | Nexus user token passcode — second part of the token pair. Sent as the HTTP Basic Auth password. |
| `NEXUS_HTTP_TIMEOUT` | No | `30s` | Timeout for HTTP requests, parsed as a Go `time.Duration`. |
| `NEXUS_SPECIFIC_REPOSITORY` | No | _(empty)_ | Restrict sync to this single repository name. When not set, all repositories are iterated. |
| `NEXUS_WEBHOOK_SECRET` | No | _(empty)_ | HMAC-SHA1 secret for webhook signature validation. When set, every inbound request must carry a valid `X-Nexus-Webhook-Signature` header. When empty, signature validation is skipped. |
| `NEXUS_WEBHOOK_PATH` | No | `/nexus/webhook` | HTTP path on which the webhook listener accepts inbound events. |

## Supported Data Types

| Type | Sync | Webhook |
| --- | --- | --- |
| `dockerimage` | ✅ | ✅ |
| `componentasset` | ✅ | ✅ |

### `dockerimage` (sync only)

One entry per Docker component. Each item contains the component-level fields `host`, `name`,
and `version`. Available in sync mode only — the Nexus webhook payload does not carry the
information required to populate this type.

### `componentasset` (sync and webhook)

Component assets with a fan-out design. For each component, the source emits one item per asset.
Each item contains the component-level fields (`host`, `id`, `repository`, `format`, `group`,
`name`, `version`, `tags`) plus a single `asset` object with the asset details. Mapping templates
access asset fields via `{{ .asset.fieldName }}`.

In sync mode the source operates on Docker repositories only — non-Docker components are skipped.
In webhook mode all component formats are processed.

## Authentication

The source authenticates using a [Nexus user token](https://help.sonatype.com/en/user-tokens.html)
pair via HTTP Basic Auth. `NEXUS_TOKEN_NAME` is sent as the username and `NEXUS_TOKEN_PASSCODE`
as the password.

To create a user token, navigate to your Nexus user profile and select **User Token** in the
left-hand menu. Click **Access user token** and copy the token name code and the token pass code.

The token must have read permissions on the repositories and components you intend to synchronise.

## Webhook Events

The webhook listener registers on `X-Nexus-Webhook-Id: rm:repository:component` events only.
All other event types are silently ignored.

| Action | Operation |
| --- | --- |
| `CREATED` | Upsert — fetches full component details from the REST API and emits one `componentasset` entry per asset. If the API call fails, the data from the webhook payload is used as a fallback. |
| `DELETED` | Delete — emits a single `componentasset` delete using the identifiers from the webhook payload. |

### Signature Validation

When `NEXUS_WEBHOOK_SECRET` is set, each inbound request is validated against the
`X-Nexus-Webhook-Signature` header using HMAC-SHA1. Requests with a missing or invalid signature
are rejected with an HTTP error.

When `NEXUS_WEBHOOK_SECRET` is not set, signature validation is disabled and all requests are
accepted. This is useful for internal deployments where network-level controls are sufficient.

To enable Nexus to send a signature, configure the **Secret Key** field when creating the webhook
in the Nexus administration UI.

## Example Mapping Files

Example mapping files are provided in the `docs/examples/nexus/mappings/` directory:

- `dockerimages.yaml` — maps Docker image components to Catalog items.
- `componentassets.yaml` — maps component assets to Catalog items.

These files can be used as a starting point for your own mapping configurations. Pass the folder
or a specific file to the `--mapping-file` flag:

```sh
ibdm sync nexus --mapping-file docs/examples/nexus/mappings/
```

For local development and debugging, add the `--local-output` flag to send results to stdout:

```sh
ibdm sync nexus --mapping-file docs/examples/nexus/mappings/ --local-output
```
