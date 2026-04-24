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

### `dockerimage`

One entry per Docker image asset. The source fans out component assets: for each Docker component
it emits one `dockerimage` item per asset. Each item contains component-level fields (`host`,
`name`, `version`, `repository`, `format`, `tags`) plus a single `asset` object with asset-level
details (`downloadUrl`, `checksum.sha256`, `lastModified`, `lastDownloaded`, etc.). Mapping
templates access asset fields via `{{ .asset.fieldName }}`. Available in both sync and webhook modes.

In both sync and webhook modes the source operates on Docker repositories only — non-Docker
components are skipped.

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
| `CREATED` | Upsert — fetches full component details from the REST API and emits one `dockerimage` entry per asset. If the API call fails, the event is logged and skipped. |
| `UPDATED` | Upsert — same behaviour as `CREATED`: fetches full component details and emits one `dockerimage` entry per asset. |
| `DELETED` | Delete — emits one `dockerimage` delete using the `host`, `name`, and `version` from the webhook payload. No API call is made. |

The event time recorded on each emitted item is taken from the `timestamp` field of the
webhook payload (RFC 3339 format). Events with a missing or unparsable timestamp are skipped.

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

- `dockerimages.yaml` — maps Docker image assets to Catalog items.

This file can be used as a starting point for your own mapping configuration. Pass the file
or the folder to the `--mapping-file` flag:

```sh
ibdm sync nexus --mapping-file docs/examples/nexus/mappings/
```

For local development and debugging, add the `--local-output` flag to send results to stdout:

```sh
ibdm sync nexus --mapping-file docs/examples/nexus/mappings/ --local-output
```
