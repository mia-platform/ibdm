# GitLab Integration

The GitLab Integration of `ibdm` can work in two modes:

- receiving webhook events (Pipeline Hook, Push Hook)
- getting resources via the GitLab REST API

## Commands

The commands below use every internal mapping shipped for the integration. To choose which of
them run, or to add your own mappings, see
[Internal and External Mappings](./015_internal-and-external-mappings.md).

Once you have the `ibdm` binary available the run of the integration is straightforward.

If you want to start a new integration that exposes a webhook endpoint for receiving GitLab events
you can run the following command:

```sh
ibdm run gitlab --include-internal-mappings=all
```

If you want to start a full sync process that fetches resources from the GitLab API run this
instead:

```sh
ibdm sync gitlab --include-internal-mappings=all
```

## Configurations

In addition to other environment variables the GitLab source requires or accepts the following:

- `GITLAB_TOKEN` (**required**): a GitLab personal access token or project/group access token used
  to authenticate all REST API requests
- `GITLAB_BASE_URL` (**required**): the base URL of the GitLab instance (e.g. `https://gitlab.com`)
- `GITLAB_WEBHOOK_PATH`: the path where the webhook handler will be exposed (default:
  `/gitlab/webhook`)
- `GITLAB_WEBHOOK_TOKEN`: the secret token configured in the GitLab webhook settings — used to
  validate incoming requests via the `X-Gitlab-Token` header

The first two variables are used for both sync and run modes.
`GITLAB_WEBHOOK_PATH` and `GITLAB_WEBHOOK_TOKEN` are only needed when running in webhook mode
(`run`). `GITLAB_WEBHOOK_TOKEN` is required in that mode: if it is not set, `ibdm run gitlab`
fails to start with a `webhook token not configured` error.

## Supported Data Types

The source supports three data types that can be used in mapping files:

- `project` — GitLab projects fetched via the REST API. Each item contains a `project` object with
  the full project payload and a `project_languages` object with the language usage breakdown.
- `pipeline` — Pipelines for each project. Each item contains a `project` object and a `pipeline`
  object with the full pipeline details fetched from the single-pipeline API endpoint.
- `accesstoken` — Access tokens for projects and groups. Each item contains either a `project` or
  `group` object alongside a `token` object with the access token details.

### Sync Mode

In sync mode, `project` is the primary resource. When a mapping of type `project` is loaded,
the source iterates all accessible GitLab projects and, for each project, optionally fetches:

- project access tokens (if `accesstoken` is also mapped)
- project pipelines (if `pipeline` is also mapped)

When `accesstoken` is mapped, the source also independently iterates all accessible groups and
fetches their group-level access tokens.

### Webhook Mode

In webhook mode, the source currently handles two GitLab event types:

- **Pipeline Hook** — triggers on pipeline events. Emits both a `project` and a `pipeline` data
  item when both types are loaded.
- **Push Hook** — triggers on push events. Emits a `project` data item with the updated project
  information.

## Authentication

The source authenticates to the GitLab REST API using a token passed via the `GITLAB_TOKEN`
environment variable. The token is attached to every HTTP request as a `PRIVATE-TOKEN` header.

The token must have read permissions on the projects, pipelines, and access tokens you intend to
synchronize.

## Internal Mappings

`ibdm` ships these internal mappings for GitLab (`ibdm mappings list gitlab`):

- `projects` — maps GitLab projects to Catalog items.
- `pipelines` — maps pipelines to Catalog items.
- `accesstokens` — maps access tokens to Catalog items.

Select them with `--include-internal-mappings`, for example the projects and their pipelines only:

```sh
ibdm sync gitlab --include-internal-mappings=projects,pipelines
```

For local development and debugging, add the `--local-output` flag to send results to stdout:

```sh
ibdm sync gitlab --include-internal-mappings=all --local-output
```

Pipelines are only fetched while the projects are walked, so `--include-internal-mappings=pipelines`
alone produces nothing: see the dependencies described above.
