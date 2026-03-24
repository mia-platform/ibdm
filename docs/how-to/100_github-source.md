# GitHub Integration

The GitHub Integration of `ibdm` supports two modes:

- **Sync mode**: fetches repository data on demand via the [GitHub REST API](https://docs.github.com/en/rest).
- **Webhook mode**: listens for inbound GitHub webhook events and updates the Catalog in real time.

## Commands

Once you have the `ibdm` binary available the run of the integration is straightforward.

To start a one-off sync that queries the GitHub API:

```sh
ibdm sync github --mapping-file <path to mapping file or folder>
```

To start a long-running webhook listener that receives GitHub events:

```sh
ibdm run github --mapping-file <path to mapping file or folder>
```

## Configuration

In addition to other environment variables the GitHub source requires or accepts the following:

### Required

| Variable | Description |
| --- | --- |
| `GITHUB_TOKEN` | GitHub personal access token (classic) or fine-grained token with appropriate scopes. |
| `GITHUB_ORG` | The GitHub organization name to synchronize. |

### Optional

| Variable | Default | Description |
| --- | --- | --- |
| `GITHUB_URL` | `https://api.github.com` | Base URL of the GitHub API. Override for GitHub Enterprise Server. |
| `GITHUB_HTTP_TIMEOUT` | `30s` | HTTP request timeout (Go duration format). |
| `GITHUB_PAGE_SIZE` | `100` | Items per API page (1–100). |
| `GITHUB_WEBHOOK_SECRET` | _(empty)_ | HMAC secret for webhook signature verification. Required for webhook mode. |
| `GITHUB_WEBHOOK_PATH` | `/webhook/github` | HTTP path for incoming webhook events. |

## Authentication

### Personal Access Token (Classic)

Required scopes: `repo` and `read:org`.

### Fine-Grained Personal Access Token

Required permissions at the organization level:

- Repository > Metadata: Read-only
- Organization > Members: Read-only

## Supported Data Types

| Type | Sync | Webhook |
| --- | --- | --- |
| `repository` | ✅ | ✅ |

The same mapping configuration works for both sync and webhook modes.

## Setting Up a GitHub Webhook

To use webhook mode, configure a GitHub organization webhook:

1. Go to your GitHub organization's **Settings > Webhooks > Add webhook**.
1. Set the **Payload URL** to your IBDM public URL at the configured webhook path (default: `/webhook/github`).
1. Set **Content type** to `application/json`.
1. Enter a **Secret** matching your `GITHUB_WEBHOOK_SECRET` environment variable.
1. Select the events you want to receive (e.g., **Repositories**).

## GitHub Enterprise Server

Override `GITHUB_URL` to point to your GitHub Enterprise instance:

```sh
export GITHUB_URL=https://github.example.com/api/v3
```

## Example

```sh
export GITHUB_TOKEN="ghp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
export GITHUB_ORG="mia-platform"
export GITHUB_WEBHOOK_SECRET="my-webhook-secret"

ibdm sync github --mapping-file ./mappings/github/
```
