# Sysdig Secure Integration

The Sysdig Secure Integration of `ibdm` fetches vulnerability data for container images via the
SysQL API.

Only sync mode is supported — no event streaming or webhook mode is available.

## Commands

Once you have the `ibdm` binary available the run of the integration is straightforward.

To start a sync process that queries the Sysdig SysQL API:

```sh
ibdm sync sysdig --mapping-file <path to mapping file or folder>
```

## Configuration

In addition to other environment variables the Sysdig source requires or accepts the following:

- `SYSDIG_URL` (**required**): the base URL of the Sysdig Secure instance
  (e.g., `https://secure.sysdig.com`)
- `SYSDIG_API_TOKEN` (**required**): a Sysdig API bearer token — this is a secret and must never
  appear in logs or error messages
- `SYSDIG_HTTP_TIMEOUT`: timeout for HTTP requests to the Sysdig API, parsed as a Go
  `time.Duration` (default: `30s`)
- `SYSDIG_PAGE_SIZE`: number of items per SysQL query page (default: `1000`, must be between 1
  and the internal maximum)

## Authentication

The source authenticates to the Sysdig API using a service-level bearer token passed via the
`SYSDIG_API_TOKEN` environment variable. The token is attached to every HTTP request as an
`Authorization: Bearer <token>` header.

Ensure the token has read permissions for vulnerability and image data in Sysdig Secure.

## Data Types

The Sysdig source currently supports a single data type:

- `vulnerability`: fetches all image vulnerabilities via a full SysQL scan. Each result row
  contains a `vuln` object with vulnerability details and an `img` object with image metadata.
  Mapping templates access fields via alias paths such as `.vuln.severity` or
  `.img.imageReference`.
