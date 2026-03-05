# Sonatype Nexus Repository Manager Integration

The Sonatype Nexus Repository Manager Integration of `ibdm` works in sync mode only, getting
resources via the [Nexus REST API](https://help.sonatype.com/en/automation.html#rest-api).

## Commands

Once you have the `ibdm` binary available the run of the integration is straightforward.

If you want to start a Nexus sync process run this command:

```sh
ibdm sync nexus --mapping-file <path to mapping file or folder>
```

## Configurations

In addition to other environment variables the Nexus source requires these additional ones:

- `NEXUS_URL_SCHEMA`: the URL scheme of the Nexus Repository Manager instance (e.g. `https`).
- `NEXUS_URL_HOST`: the hostname (and optional port) of the Nexus Repository Manager instance
	(e.g. `nexus.example.com`). No scheme, no trailing slash.
- `NEXUS_TOKEN_NAME`: Nexus user token name — the first part of a
	[Nexus user token](https://help.sonatype.com/en/user-tokens.html) pair. Used as the username
	in HTTP Basic Auth.
- `NEXUS_TOKEN_PASSCODE`: Nexus user token passcode — the second part of the token pair. Used as
	the password in HTTP Basic Auth.

The following environment variables are optional:

- `NEXUS_HTTP_TIMEOUT`: timeout for HTTP requests, parsed as a Go `time.Duration` (default: `30s`).
- `NEXUS_SPECIFIC_REPOSITORY`: if set, restricts synchronization to this single repository name.
	When not set, the source lists all repositories and iterates through each one.

## Supported Data Types

The source operates on Docker repositories only — non-Docker components are skipped. It supports
two data types that can be used in mapping files:

- `dockerimage` — One entry per Docker component. Each item contains the component-level fields
	`host`, `name`, and `version`.
- `componentasset` — Component assets with a fan-out design. For each Docker component, the source
	emits one item per asset. Each item contains the component-level fields (`host`, `id`,
	`repository`, `format`, `group`, `name`, `version`, `tags`) plus a single `asset` object with
	the asset details. Mapping templates access asset fields via `{{ .asset.fieldName }}`.

## Authentication

The source authenticates using a `Nexus user token` pair via HTTP Basic Auth.
The `NEXUS_TOKEN_NAME` is sent as the Basic Auth username and
`NEXUS_TOKEN_PASSCODE` as the password.

To create a user token, navigate to your Nexus user profile and select **User Token** in the
left-hand menu. Click **Access user token** and copy the token name code and the token pass code.

The token must have read permissions on the repositories and components you intend to synchronize.

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
