# SonarQube Integration

The SonarQube Integration of `ibdm` records [SonarQube] analyses in the Mia-Platform Catalog: one
item per analysis run, one item per issue, and a relationship from each issue to the latest run
that reported it. It supports two modes:

- **Run (Webhook)** — push-based: starts an HTTP server that receives the webhook SonarQube sends
  when an analysis finishes, records the run, reads the issues of the analysed branch or pull
  request from the SonarQube Web API, and forwards each of them to the Catalog.
- **Sync** — pull-based: records the latest run and the current issues of the main branch of every
  project, and exits.

The webhook payload announces that an analysis finished and reports the quality gate, but it does
not carry the findings. Every delivery therefore becomes a read of `GET /api/issues/search` with
the token configured here. The `serverUrl` in a payload is never followed: the Web API is always
called on `SONARQUBE_URL`.

Verified against SonarQube Server 26.5.

## Commands

### Run (Webhook Listener)

```sh
ibdm run sonarqube --mapping-file <path to mapping file or folder>
```

### Sync

```sh
ibdm sync sonarqube --mapping-file <path to mapping file or folder>
```

## Configuration

All configuration is read from environment variables.

### Environment Variables

| Env Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `SONARQUBE_URL` | Yes | _(empty)_ | Base URL of the SonarQube server every Web API call is made to (e.g. `https://sonarqube.example.com`). |
| `SONARQUBE_TOKEN` | Yes | _(empty)_ | A global analysis token, or a user token with _Browse_ on the analysed projects. Sent as `Authorization: Bearer <token>`. |
| `SONARQUBE_PUBLIC_URL` | No | _(empty)_ | Address a person opens SonarQube at, used for the link on each item. When empty, the payload `serverUrl` is used for webhooks and `SONARQUBE_URL` for sync. Set it when `SONARQUBE_URL` is an in-cluster address. |
| `SONARQUBE_HTTP_TIMEOUT` | No | `30s` | Timeout for HTTP requests, as a Go `time.Duration`. |
| `SONARQUBE_ISSUE_STATUSES` | No | `OPEN,CONFIRMED` | `issueStatuses` filter of the issues read. Add `ACCEPTED` to also record findings somebody has consciously accepted. |
| `SONARQUBE_NEW_CODE_ONLY` | No | `false` | Read only the issues in the project new code period. |
| `SONARQUBE_PAGE_SIZE` | No | `500` | Issues per page (1–500). |
| `SONARQUBE_MAX_ISSUES` | No | `20000` | Ceiling on the issues read for one project ref. |
| `SONARQUBE_PROJECT_KEYS` | No | _(empty)_ | Comma-separated project keys to sync. When empty, sync reads every project the token can browse. |
| `SONARQUBE_WEBHOOK_PATH` | No | `/sonarqube/webhook` | HTTP path on which the webhook listener accepts deliveries. |
| `SONARQUBE_WEBHOOK_SECRET` | **Webhook** | _(empty)_ | The secret configured on the SonarQube webhook. Required unless `SONARQUBE_WEBHOOK_ALLOW_UNSIGNED` is `true`. |
| `SONARQUBE_WEBHOOK_ALLOW_UNSIGNED` | No | `false` | Accept deliveries without a signature when no secret is configured. For a local loop only. |
| `SONARQUBE_SCM_PROVIDER` | No | `auto` | Forge the analysed repositories are on: `auto`, `gitlab`, `github`, `bitbucket`, `bitbucket-server`, `azure-devops`, `custom` or `none`. See [Links to the Source Code](#links-to-the-source-code). |
| `SONARQUBE_SCM_ANALYSIS_PROPERTY` | No | `sonar.analysis.repoUrl` | Analysis property a repository URL may arrive in. |
| `SONARQUBE_SCM_USE_PROJECT_LINKS` | No | `true` | Whether to read the project `sonar.links.scm` link from SonarQube. |
| `SONARQUBE_SCM_REPOSITORY_URL` | No | _(empty)_ | Fixed repository URL, for a deployment watching a single repository. |
| `SONARQUBE_SCM_URL_TEMPLATE` | No | _(empty)_ | File URL template overriding the built-in shape. Placeholders: `{repo}`, `{ref}`, `{path}`. |
| `SONARQUBE_SCM_LINE_TEMPLATE` | No | _(empty)_ | Line anchor appended to the file URL. Placeholder: `{line}`. |

## Setting Up the SonarQube Webhook

In SonarQube, go to _Administration → Configuration → Webhooks_ (globally, or per project) and
create a webhook with:

- **URL** — where `ibdm` is published, plus `SONARQUBE_WEBHOOK_PATH`.
- **Secret** — the same value as `SONARQUBE_WEBHOOK_SECRET`.

SonarQube signs every delivery with `hex(hmac_sha256(secret, body))` in the
`X-Sonar-Webhook-HMAC-SHA256` header, and `ibdm` refuses a delivery whose signature is missing or
does not match. SonarQube holds no token the platform issues, so this signature is the only
authentication of the route: publish it unauthenticated at the edge, and keep the secret set in
any deployment that is reachable.

The webhook is answered as soon as the signature is verified and the body parsed, and the issues
are read afterwards: SonarQube times a webhook out after ten seconds, and reading a large project
takes longer. The outcome of each delivery is reported in the `ibdm` logs.

## Supported Data Types

| Type | Sync | Webhook |
| --- | --- | --- |
| `issue` | ✅ | ✅ |
| `run` | ✅ | ✅ |

Map both: an issue is related to its run only when runs are mapped too.

## Webhook Events

| Delivery | Produces |
| --- | --- |
| `status` is `SUCCESS` | one `run` upsert, then one `issue` upsert per issue of the analysed branch or pull request |
| any other `status` | one `run` upsert, recording the failure. No issue is read: the analysis did not complete, and reading the project would record the previous analysis' findings |

The run is sent before its issues, so the item their relationships point at is written first. When
the issues cannot be read, the run is still recorded, without counts, and the error is logged.

A branch analysis reads the issues of that branch, a pull request analysis those of the pull
request, and a payload naming no branch those of the main branch. The event time is the analysis
`analysedAt`.

## Sync

For each project, sync reads the issues of the main branch and looks its name up with
`/api/project_branches/list`, so that the items carry the same branch as those a webhook delivery
writes. The latest analysis comes from `/api/project_analyses/search`: its date and revision are
those a webhook delivery for it carries, so sync and webhook write the same run, and SCM links point
at the analysed commit. Its quality gate comes from `/api/qualitygates/project_status` and
`/api/qualitygates/get_by_project`, best effort. Other branches and pull requests are only recorded
through webhooks.

The token needs _Browse_ on the projects. Projects are listed with `/api/components/search`, so
only those the token can see are synced.

A run written by a webhook and then by a sync keeps the sync's view: the task id, which only a
webhook knows, becomes null, and the analysis key, which only a sync knows, is set.

## Large Projects

`/api/issues/search` refuses to paginate past 10 000 results. Past that, the read is split per
impact severity, worst first, and the results are de-duplicated by issue key.
`SONARQUBE_MAX_ISSUES` caps the whole read. A read that stopped short is logged as a warning.

## Links to the Source Code

Each item links to the finding in SonarQube and, when it can be built, to the offending line in
the repository. SonarQube reports a path and a line but never says which repository they are in,
so the repository URL is resolved once per analysis, from the first of these that answers:

1. **the analysis property** `SONARQUBE_SCM_ANALYSIS_PROPERTY`, passed by the CI job that ran the
   scan: `sonar-scanner -Dsonar.analysis.repoUrl="$CI_PROJECT_URL"`. Webhook only;
1. **the project `scm` link** in SonarQube (`sonar.links.scm`), read from
   `/api/project_links/search` when `SONARQUBE_SCM_USE_PROJECT_LINKS` is `true`. The Maven form
   `scm:git:https://…` and the SSH form `git@host:group/repo.git` are normalised;
1. **`SONARQUBE_SCM_REPOSITORY_URL`**.

`SONARQUBE_SCM_PROVIDER` decides the shape of the link. `auto` recognises only the hosted services
(`gitlab.com`, `github.com`, `bitbucket.org`, `dev.azure.com`): a self-hosted forge must be named,
otherwise its items carry no line link.

| Provider | A line of a file |
| --- | --- |
| `gitlab` | `{repo}/-/blob/{ref}/{path}#L{line}` |
| `github` | `{repo}/blob/{ref}/{path}#L{line}` |
| `bitbucket` | `{repo}/src/{ref}/{path}#lines-{line}` |
| `bitbucket-server` | `{repo}/browse/{path}?at={ref}#{line}` |
| `azure-devops` | `{repo}?path=/{path}&version=GC{ref}&line={line}` |
| `custom` | `SONARQUBE_SCM_URL_TEMPLATE` + `SONARQUBE_SCM_LINE_TEMPLATE` |

The reference is the analysed commit (`revision`) whenever the payload carries one, and the branch
only as a fallback: a link to a branch points at whatever is on it now. A pull request analysis
without a revision gets no line link, since its branch name is the pull request key. So does an
Azure DevOps repository, whose `GC` prefix can only address a commit. Sync knows no revision, so
its links point at the main branch.

A repository that cannot be resolved costs the link and nothing else.

## Data Structure

Each `issue` exposes the following values in the mapping context:

- `.issue` — the issue as `/api/issues/search` returns it (`key`, `rule`, `message`, `component`,
  `line`, `textRange`, `impacts`, `tags`, …).
- `.rule` — the rule it cites, from the `rules` block of the same response (`key`, `name`,
  `langName`), or an empty object.
- `.analysis` — the analysis it was reported by: `serverUrl`, `projectKey`, `projectName`,
  `branch`, `pullRequest`, `isMainBranch`, `taskId`, `analysedAt`, `revision`,
  `qualityGateStatus`, `qualityGateName`.
- `.derived` — values computed by `ibdm`:
  - `projectSlug` — the project key reduced to an item name prefix;
  - `title`, `description` — the message (or rule), and `rule — path:line`;
  - `severity` — the worst of the issue impacts, or the legacy severity;
  - `status` — `issueStatus`, or the legacy `status`;
  - `componentPath`, `effort`;
  - `creationDate`, `updateDate`, `closeDate` — normalised to RFC 3339;
  - `sonarqubeUrl`, `repositoryUrl`, `scmUrl` — see above;
  - `labels`, `tags`, `links` — ready for the item metadata, already reduced to what the Catalog
    accepts.

Every key of `.analysis` and `.derived` is always present, `null` when unknown.
`.analysis.runKey` identifies the run the issue belongs to, and is `null` when runs are not mapped.

Each `run` exposes:

- `.analysis` — the same values as for an issue, `runKey` included: the project, the ref (empty for
  the main branch) and the analysis date. A webhook delivery and a sync of the same analysis have
  the same `runKey`.
- `.run` — `status` (`SUCCESS`, `FAILED` or `CANCELED`), `analysisKey` and `projectVersion` (sync
  only), `qualityGateConditions` (`metric`, `operator`, `status`, `value`, `errorThreshold`, the
  same shape whichever API reported them), and what was read: `issuesRead`, `truncated` and
  `issueCounts` by severity, all `null` when the issues were not read.
- `.derived` — `projectSlug`, `title`, `description`, `sonarqubeUrl` (the dashboard of the analysed
  ref), `repositoryUrl`, and `labels`, `tags` and `links` for the item metadata.

## Metadata Is Written Once

The Catalog stores the `metadata` of an item written by `ibdm` when the item is created, and keeps
it on every update: only the `spec` is replaced. So the example mappings put in labels and tags only
facts that never change. For an issue that means project, branch, pull request and type. For a run
it also means the task status and quality gate, which are final once the run happened. What changes
from one analysis to the next, like the status and severity of an issue, lives in the `spec`, whose
`issueStatus` and `severity` are selectable fields. For the same reason an issue's link to the
repository keeps pointing at the commit of the analysis that first reported it; `spec.scmUrl`
follows the latest one.

## Example Mapping Files

Example mapping files are provided in the `docs/mappings/sonarqube/` directory:

- `runs.yaml` — maps each run to an item of the `runs.sonarqube.mia-platform.eu` Item Type
  Definition.
- `issues.yaml` — maps each issue to an item of the `issues.sonarqube.mia-platform.eu` Item Type
  Definition, and relates it to its run with a `part-of.mia-platform.eu` relationship: the issue
  _is part of_ the run, the run _contains_ the issue.

Item names are the project slug plus the first 16 hexadecimal characters of the SHA-256 of the
issue key, or of the run key: stable, so a re-analysis updates an issue rather than adding another,
and a webhook and a sync of the same analysis write the same run.

The relationship identifier depends on the issue alone, so the Catalog keeps one relationship per
issue, pointing at the latest run that reported it: every analysis moves it rather than adding one.
To keep the whole history, one relationship per issue and run, hash the `targetRef` URN into the
identifier as well. Expect one relationship per issue per analysis then.

```sh
ibdm run sonarqube --mapping-file docs/mappings/sonarqube/
```

For local development and debugging, add the `--local-output` flag to send results to stdout:

```sh
ibdm sync sonarqube --mapping-file docs/mappings/sonarqube/ --local-output
```

## Not in Scope

- **Security hotspots.** They are not returned by `/api/issues/search`.
- **Removing fixed issues.** Items are upserted; an issue that is fixed or closed is not deleted
  from the Catalog. Neither are its relationship nor old runs.

[SonarQube]: https://docs.sonarsource.com/sonarqube-server/
