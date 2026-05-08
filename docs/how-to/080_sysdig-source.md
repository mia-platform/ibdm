# Sysdig Secure Integration

The Sysdig Secure Integration of `ibdm` fetches vulnerability data for container images. It
supports two modes:

- **Sync** — pull-based: queries the Sysdig SysQL API on demand and exits.
- **Run (Webhook)** — push-based: starts an HTTP server that receives pipeline scan failure
  notifications from Sysdig, calls the Sysdig Vulnerability API to retrieve the full scan result,
  and forwards each vulnerability to the Catalog.

## Commands

### Sync

```sh
ibdm sync sysdig --mapping-file <path to mapping file or folder>
```

Performs a one-off synchronisation: queries the Sysdig SysQL API for all image vulnerabilities
and exits.

### Run (Webhook Listener)

```sh
ibdm run sysdig --mapping-file <path to mapping file or folder>
```

Starts a long-running HTTP server that listens for inbound Sysdig pipeline failure notifications.
For each notification, IBDM calls the Sysdig Vulnerability API to retrieve the full scan result
and forwards each vulnerability to the pipeline.

## Configuration

All configuration is read from environment variables.

### Environment Variables

| Env Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `SYSDIG_URL` | **Sync** | _(empty)_ | Base URL of the Sysdig Secure instance (e.g. `https://secure.sysdig.com`). |
| `SYSDIG_API_TOKEN` | **Sync** | _(empty)_ | Sysdig API bearer token for the SysQL API. Secret — never appears in logs. |
| `SYSDIG_HTTP_TIMEOUT` | No | `30s` | Timeout for HTTP requests, as a Go `time.Duration`. |
| `SYSDIG_PAGE_SIZE` | No | `1000` | Number of items per SysQL query page (1–1000). |
| `SYSDIG_BASE_URL` | **Webhook** | _(empty)_ | Base URL of the Sysdig Vulnerability API for the account's region (see below). |
| `SYSDIG_BEARER_TOKEN` | **Webhook** | _(empty)_ | Bearer token for the Sysdig Vulnerability API. Secret — never appears in logs. |
| `SYSDIG_WEBHOOK_URL` | No | `/sysdig/webhook` | HTTP path on which IBDM listens for incoming Sysdig notifications. |

### Region Base URLs

`SYSDIG_BASE_URL` must match the region where the Sysdig account is hosted:

| Region | Base URL |
| --- | --- |
| US East | `https://app.sysdigcloud.com` |
| US West | `https://us2.app.sysdig.com` |
| EU | `https://eu1.app.sysdig.com` |
| AP (Australia) | `https://app.au1.sysdig.com` |

## Authentication

### Sync

Authenticates with the SysQL API using `SYSDIG_API_TOKEN`, sent as
`Authorization: Bearer <token>` on every request.

### Webhook

Authenticates with the Vulnerability API using `SYSDIG_BEARER_TOKEN`, sent as
`Authorization: Bearer <token>` on every enrichment request.

Incoming webhook requests from Sysdig carry no signature — no shared secret is required.

## Supported Data Types

| Type | Sync | Webhook |
| --- | --- | --- |
| `vulnerability` | ✅ | ✅ |

## Webhook Events

IBDM processes Sysdig webhook notifications whose `event.id` **or** `event.eventData.name` equals
`Pipeline Failure Alerts`. All other events are silently ignored.

| Event | Produces |
| --- | --- |
| `Pipeline Failure Alerts` | `vulnerability` upsert (one per vulnerability) |

When a matching notification arrives, IBDM:

1. Extracts the result ID from the `event.url` field (the segment between `results/` and `/overview`).
1. Calls `GET /secure/vulnerability/v1beta1/results/{resultId}` on `SYSDIG_BASE_URL` to retrieve
   the full scan result.
1. Skips the result if `result.type` is not `dockerImage` or `result.policyEvaluationsResult` is
   not `failed`.
1. Emits one `vulnerability` upsert per vulnerability entry, with the following structure:

```json
{
  "vuln": { "<full vulnerability object>" },
  "img": { "imageReference": "<pullString>" }
}
```

The event timestamp is derived from the notification's `timestamp` field (microseconds, converted
to milliseconds).

## Data Structure

Each `vulnerability` item emitted by both sync and webhook modes exposes the same fields in the
mapping context:

- `.vuln` — full vulnerability object (name, severity, CVSS score, dates, exploitability, …)
- `.img.imageReference` — container image pull string (e.g. `registry.example.com/app:v1.0.0`)
