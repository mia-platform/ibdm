# External Attack Surface Management (EASM) Integration

The EASM Integration of `ibdm` connects to the EASM backend via its REST API and reads a
customer's latest completed scan run. It supports pull-based sync only.

## Commands

### Sync

```sh
ibdm sync easm --mapping-file <path to mapping file or folder>
```

Performs a one-off synchronisation: fetches the customer's latest completed run from the EASM
`/data` endpoint as a single cursor-paginated list, emits one item per record routed by the
record's own `type` field, and exits.

## Configuration

All configuration is read from environment variables.

### Environment Variables

| Env Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `EASM_BASE_URL` | Yes | _(empty)_ | Base URL of the EASM backend (e.g. `https://easm.example.com`). |
| `EASM_CUSTOMER` | Yes | _(empty)_ | Customer identifier. Scopes the request to a single customer via the `X-Customer` header — it selects whose scan results to read. |
| `EASM_TOKEN` | No | _(empty)_ | Bearer token authenticating the caller. When set, it is sent as `Authorization: Bearer <token>`. |
| `EASM_DATA_PATH` | No | `/data` | Path of the read endpoint appended to `EASM_BASE_URL`. |
| `EASM_HTTP_TIMEOUT` | No | `30s` | Timeout for each HTTP request, parsed as a Go `time.Duration`. |

## Supported Data Types

| Type | Sync |
| --- | --- |
| `domain` | ✅ |
| `endpoint` | ✅ |
| `host` | ✅ |
| `ip` | ✅ |
| `vulnerability` | ✅ |

The endpoint tags each record with one of these types; records with a missing or empty `type`
are skipped. Each emitted item carries the record's fields unchanged, and the pipeline can restrict
a run to a subset of types via the mapping files — only the requested types are emitted.

### `domain`

One entry per discovered domain, including its DNS, WHOIS, zone-transfer, Azure, and
misconfiguration data.

### `endpoint`

One entry per discovered endpoint.

### `host`

One entry per discovered host.

### `ip`

One entry per discovered IP address.

### `vulnerability`

One entry per discovered vulnerability.

## Authentication

The source scopes every request to a single customer with the `X-Customer` header, taken from
`EASM_CUSTOMER` — this is always sent.

When `EASM_TOKEN` is set, the source authenticates the caller with an
`Authorization: Bearer <token>` header.

## Example Mapping Files

Example mapping files are provided in the `docs/external-sources/easm/mappings/` directory:

- `domains.yaml` — maps domain records to Catalog items.
- `endpoints.yaml` — maps endpoint records to Catalog items.
- `hosts.yaml` — maps host records to Catalog items.
- `ips.yaml` — maps IP records to Catalog items.
- `vulnerabilities.yaml` — maps vulnerability records to Catalog items.

These files can be used as a starting point for your own mapping configuration. Pass the file
or the folder to the `--mapping-file` flag:

```sh
ibdm sync easm --mapping-file docs/external-sources/easm/mappings/
```

For local development and debugging, add the `--local-output` flag to send results to stdout:

```sh
ibdm sync easm --mapping-file docs/external-sources/easm/mappings/ --local-output
```
