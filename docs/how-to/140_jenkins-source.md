# Jenkins Integration

The Jenkins Integration of `ibdm` connects to a self-hosted [Jenkins](https://www.jenkins.io/) controller
through its [JSON REST API](https://www.jenkins.io/doc/book/using/remote-access-api/).
It supports pull-based sync only.

## Commands

### Sync

```sh
ibdm sync jenkins --include-internal-mappings=all
```

Performs a one-off synchronisation: walks the folder tree of the controller, reads the requested data
and exits.

## Requirements

The integration supports Jenkins LTS 2.555.1 or later. It reads the data exposed by these plugins:

| Plugin | Used for |
| --- | --- |
| [Folders](https://plugins.jenkins.io/cloudbees-folder/) | folders and folder credentials stores |
| [Branch API](https://plugins.jenkins.io/branch-api/) and [Pipeline: Multibranch](https://plugins.jenkins.io/workflow-multibranch/) | organization folders, multibranch projects and their primary branch |
| [Pipeline: Groovy Libraries](https://plugins.jenkins.io/pipeline-groovy-lib/) | shared libraries loaded by the builds |
| [Pipeline Graph View](https://plugins.jenkins.io/pipeline-graph-view/) | stages of the builds, with their hierarchy |
| [Pipeline: Stage View](https://plugins.jenkins.io/pipeline-stage-view/) | stages of the builds, and structured failure details |
| [Credentials](https://plugins.jenkins.io/credentials/) | credentials metadata and usage tracking |

The stages are read from Pipeline Graph View or Pipeline: Stage View: at least one of them is needed
for `build.stages`. See [Pipeline Graph View and Pipeline: Stage View](#pipeline-graph-view-and-pipeline-stage-view)
for how the source chooses between them and what each one provides.

## Configuration

All configuration is read from environment variables.

### Environment Variables

| Env Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `JENKINS_URL` | Yes | _(empty)_ | URL of the controller, with scheme and an optional path (e.g. `https://ci.example.com` or `https://ci.example.com/jenkins`). It must not include user information, a query or a fragment. |
| `JENKINS_USERNAME` | Yes | _(empty)_ | Jenkins user that owns the API token. Sent as the HTTP Basic Auth username. |
| `JENKINS_API_TOKEN` | Yes | _(empty)_ | [API token](https://www.jenkins.io/doc/book/system-administration/authenticating-scripted-clients/) of the user. Sent as the HTTP Basic Auth password and never logged. |
| `JENKINS_HTTP_TIMEOUT` | No | `30s` | Timeout for HTTP requests, parsed as a Go `time.Duration`. Must be positive. |

## Authentication

The integration authenticates with a username and an API token through HTTP Basic Auth. To create a
token, open the user menu in Jenkins, select **Security**, and add a new token under **API Token**.

The user only sees what its permissions allow. The minimum permissions are:

| Permission | Needed for |
| --- | --- |
| Overall/Read | every request |
| Job/Read | folders, multibranch projects, workflows, builds and stages |
| Credentials/View | credentials metadata, at system level and on every folder to read |
| Overall/Administer or Overall/SystemRead | the `plugin` data type; optional for detecting the stage plugins |

When the plugins cannot be listed, the sync reports the error and still emits the other data types.

Every request is built from `JENKINS_URL`: the URLs returned by Jenkins are only reported in the
data and never called, so the credentials are only sent to the configured controller.

## Supported Data Types

| Type | Sync |
| --- | --- |
| `instance` | ✅ |
| `folder` | ✅ |
| `multibranch` | ✅ |
| `workflow` | ✅ |
| `sharedlibrary` | ✅ |
| `credential` | ✅ |
| `plugin` | ✅ |

The integration assumes one Jenkins controller per Catalog: the data carry the Jenkins names
(`fullName`, credential id, plugin and library name) without a reference to the controller.

Items with a parent carry a `parent` key shaped as `{kind, fullName, url}`, where `kind` is
`instance` for the items at the top level of the controller, or `folder` for the items inside a
folder or an organization folder. Containers of unknown kind are walked through, and their children
report the closest known parent.

### `instance`

One entry for the controller.

- `instance`: `url`, `version` (read from the `X-Jenkins` response header), `mode`, `nodeDescription`,
  `useSecurity`, `quietingDown`.

### `folder`

One entry per folder and organization folder, at any depth.

- `folder`: `name`, `fullName`, `displayName`, `description`, `url`, `kind` (`folder` or
  `organization`), `objectUrl` (the URL of the SCM organization of an organization folder, empty
  otherwise).
- `parent`.

### `multibranch`

One entry per multibranch project.

- `multibranch`: `name`, `fullName`, `displayName`, `description`, `url`, `health` (list of
  `{score, description}`), `repositoryUrl`, `primaryBranch`, `branchCount`, `pullRequestCount`,
  `tagCount`.
- `parent`.

`primaryBranch` holds `name`, `fullName` and `url` of the branch the SCM marks as primary, and is
absent when no branch is marked. `branchCount`, `pullRequestCount` and `tagCount` are read from the
views of the project. All four are absent when the branches of the project cannot be listed, and the
counters are absent when the project has no such views.

The branches, pull requests and tags of a multibranch project are never emitted, except the primary
branch, which is emitted as a `workflow`. A multibranch project without a primary branch has no
workflow.

### `workflow`

One entry per Pipeline job (`WorkflowJob`): the standalone ones (`kind: standalone`) and the primary
branch of every multibranch project (`kind: primaryBranch`). Freestyle, matrix and maven jobs are
skipped.

- `workflow`: `name`, `fullName`, `displayName`, `description`, `url`, `buildable`, `kind`.
- `parent`, for standalone workflows only.
- `multibranch`: `name`, `fullName`, `url` of the project, for primary branch workflows only.
- `build`: the last completed build, absent when the workflow has none: `number`, `result`,
  `timestamp`, `duration`, `url`, and, when a stage plugin is available, `stages` and
  `stagesSource` (`pipeline-graph-view` or `pipeline-stage-view`).
- `libraries`: the shared libraries loaded by the build, as `{name, version, trusted}`.

`build.stages` is always a flat list, each stage before its children, in execution order. Every
stage carries:

- `id`: the id of the stage in the build, unique within the build;
- `name`, `startTimeMillis`, `durationMillis`;
- `status`: `success`, `failure`, `unstable`, `aborted`, `skipped`, `not_built`, `running`, `paused`,
  `queued`, `finished` or `unknown`. Stage View reports the stages skipped by a condition and those
  not reached after a failure both as `skipped`;
- `agent`: the agent the stage ran on, empty when unknown;
- `parallel`: whether the stage is a branch of a `parallel` block;
- `synthetic`: whether Jenkins generated the stage, such as `Declarative: Post Actions`;
- `children`: the ids of the child stages, so the tree can be rebuilt.

`parallel`, `synthetic` and `children` are only known with Pipeline Graph View: with Stage View they
are always `false`, `false` and empty.

The innermost failed stage, the one with no failed child, also carries `failure`:

- `message` and `type` of the error, the message truncated to 2048 characters. With Graph View alone,
  they are split from the exception text: the stack trace is dropped, and the leading fully
  qualified Java class name, when present, becomes the type;
- `nodeName`, the step that failed, as the plugin names it. Stage View reports the name of the step,
  such as `Shell Script`. Graph View reports the descriptive name of the step, such as `Git`, and,
  for steps without one such as Shell Script, its arguments or label, such as the shell command.
  An error thrown outside any step is reported as `Pipeline error`;
- `logUrl`, the page with the log of the failed step, absent for an error thrown outside any step;
- `consoleUrl`, the console of the whole build.

The log text itself is never read. When the failed step cannot be found, `failure` carries the error
recorded on the stage, and `nodeName` and `logUrl` only when Pipeline: Stage View finds the failed
step within the stage. When the error cannot be read either, `failure` carries what is known, and
always `consoleUrl`.

Every sync replaces the stages of a workflow with those of its last completed build, so renamed or
removed stages do not linger.

### `sharedlibrary`

One entry per shared library loaded by the last completed build of at least one workflow, grouped
by name.

- `library`: `name`, `versions` (every version loaded), `variables` (the global variables the
  library defines).
- `usages`: one entry per workflow loading it, with `kind`, `fullName`, `url`, `version`, `trusted`,
  `changelog`, `buildNumber` and, for primary branch workflows, `multibranch`.

### `credential`

One entry per credential of the system store and of the store of every folder and multibranch
project, in every domain. Only metadata are read: secret values and fingerprint hashes are never
requested.

- `credential`: `id`, `displayName`, `typeName`, `description`.
- `store`: `kind` (`system` or `folder`), `domain` and, for folder stores, `folder` as
  `{fullName, url}`.
- `usages`: the jobs that used the credential, as `{kind, fullName, url, folder}`.

A usage by a standalone workflow has `kind: workflow`. Usages by the branches of a multibranch
project are reported once per project, with `kind: multibranch` and the names of the branches in
`branches`.

Usages by other jobs, such as freestyle jobs or deleted jobs, are discarded. Jenkins records a usage
only when the plugin consuming the credential tracks it, as Credentials Binding (`withCredentials`),
Git and SSH Agent do.

### `plugin`

One entry per installed plugin.

- `plugin`: `shortName`, `longName`, `version`, `active`, `enabled`, `hasUpdate`, `url`,
  `requiredCoreVersion`, `dependencies` (list of `{shortName, version, optional}`).

## Pipeline Graph View and Pipeline: Stage View

Jenkins core does not expose the stages of a build: they come from one of two plugins, which describe
the same build in different ways.

| | Pipeline Graph View | Pipeline: Stage View |
| --- | --- | --- |
| Availability | suggested by the Jenkins setup wizard since 2024 | suggested before 2024, still present on many older controllers |
| Development | active | maintenance only |
| Stages | a tree: parallel branches and nested stages are children of their parent | a flat list, without hierarchy |
| Agent of a stage | yes | yes, when Jenkins records it |
| Stages generated by Jenkins | flagged as `synthetic` | not distinguishable from the others |
| Failure details | the exception as plain text | message and type as separate fields |
| Requests per workflow | 1; with a failed stage, 1 more to list the steps, plus 1 per failed stage | 1, plus 1 per failed stage |
| API | built for the pages of the plugin; it can change between plugin versions | stable for years |

### How the Source Chooses

1. Once per sync, the source reads the plugin list to know which of the two plugins are installed and
   enabled. Reading it needs the Overall/Administer or Overall/SystemRead permission: without it, each
   plugin is probed on the first build that needs it. A plugin answering `404` is considered missing
   for the rest of the sync and is never called again for the other workflows.
1. When Pipeline Graph View is available, it provides the stages, because only it exposes their
   hierarchy. When it fails on a build, that build falls back to Pipeline: Stage View.
1. When Pipeline: Stage View is available too, it also provides the error of the failed step. Both
   plugins identify stages and steps with the ids of the Pipeline engine, so the failed step found by
   Graph View can be described by Stage View.
1. When only Pipeline: Stage View is available, it provides both the stages and the errors.
1. When neither is available, workflows are emitted without `stages` and `stagesSource`, and a single
   warning is logged.

`build.stagesSource` reports which plugin provided the stages of a build, so mappings can tell
whether `children`, `parallel` and `synthetic` are meaningful.

### Known Issues of Pipeline: Stage View

- The stages are a flat list: `children` is always empty and `parallel` always `false`. Parallel
  branches and nested stages, when reported, look like sequential stages whose times overlap.
- In scripted pipelines, the branches of a `parallel` step that hold no `stage` are not stages: an
  error in one of them is reported as the failure of the stage holding the `parallel` step.
- It reports with the same `NOT_EXECUTED` status the stages skipped by a `when` condition and those
  not reached after a failure: both become `skipped`.
- Stages generated by Jenkins, such as `Declarative: Post Actions`, cannot be told apart from those
  written in the Jenkinsfile: `synthetic` is always `false`.
- Controllers set up since 2024 usually do not have it, unless it was installed on purpose.

### Known Issues of Pipeline Graph View

- The error of a failed step is only available as text. When Pipeline: Stage View is not installed,
  the source splits the text: it drops the stack trace, from the first line starting with `at` and
  a space, and when the remaining text starts with a fully qualified Java class name followed by a
  colon or by the end of the text, it takes the class name as `type` and the rest as `message`. Any
  other text becomes the whole `message`, with an empty `type`. For example:

  | Exception text | `type` | `message` |
  | --- | --- | --- |
  | `hudson.AbortException: script returned exit code 1` | `hudson.AbortException` | `script returned exit code 1` |
  | `java.io.IOException: Connection reset: by peer` | `java.io.IOException` | `Connection reset: by peer` |
  | `java.lang.InterruptedException` | `java.lang.InterruptedException` | _(empty)_ |
  | `Timeout has been exceeded` | _(empty)_ | `Timeout has been exceeded` |

- Its REST API is built for the pages of the plugin. Its responses can change between plugin
  versions, and the latest versions of the plugin require recent Jenkins weekly releases, so
  controllers on an LTS release usually run an older version.
- A stage of a sequence inside a parallel branch can be listed both among the children of the branch
  and as the next sibling of the previous stage: the source reports it once.
- The name of a failed step depends on the step: for Shell Script, the most common case, it is the
  command, because Graph View gives no descriptive name to that step.
- For a build with a failed stage, it needs one more request than Stage View, to list the steps of
  the build.

## Error Handling

A failure on one folder, workflow, stage or folder credentials store is logged and the sync moves on.
When Pipeline Graph View fails to describe a build, the stages of that build are read from Pipeline:
Stage View, when available.
A failure listing the root of the controller, the system credentials store, the plugins or the
controller itself stops that step only: the other steps still run, and the sync returns every
failure once they have.

## Limitations

### Items Are Never Deleted

The sync only creates and updates items. An item renamed, moved or removed in Jenkins stays in the
Catalog with its last known state: a job moved to another folder gets a new `fullName`, so it becomes
a new item, and the old one remains. The same applies to deleted folders, credentials and plugins.

The stages are the exception: they live inside the workflow, and every sync replaces them with those
of the last completed build.

### Only the Last Completed Build

Workflows carry their last completed build only. A build still running is not reported until it
completes, and the history of the builds is not read. The stages, the shared libraries and the
failure details all refer to that single build.

### Only Pipeline Jobs and Primary Branches

- Only Pipeline jobs (`WorkflowJob`) become workflows: freestyle, matrix and maven jobs are skipped.
- Of a multibranch project, only the primary branch becomes a workflow: the other branches, the pull
  requests and the tags are never read. The primary branch is the one the SCM marks as primary: a
  multibranch project whose SCM marks no branch, as can happen with a generic Git repository, has no
  workflow.

### Shared Libraries

Jenkins does not expose the configuration of the shared libraries through its REST API: it is only
available through the Configuration as Code export, which needs administrative permissions. The source
therefore reports the libraries loaded by the last completed builds, which has two consequences:

- a library that is configured but that no last completed build loaded is not reported;
- libraries with the same name defined in different folders are grouped in a single item, because
  the build records the name of a library, not where it is defined.

The repository of a library is not reported either.

### Credentials

Jenkins reads credentials from several providers, each with its own stores. The source reads only two
of them:

| Provider | Where the credential is defined | Read |
| --- | --- | --- |
| System | Manage Jenkins → Credentials → System | ✅ |
| Folder | on every folder and multibranch project | ✅ |
| User | in the profile of a single user | ❌ |
| External secret managers, such as Kubernetes, AWS Secrets Manager, Azure Key Vault or GCP Secret Manager credentials providers | in the external system, exposed by a plugin in a store of its own | ❌ |

As a consequence:

- the credentials of the other providers are not reported, and a workflow using only them appears in
  no `usages` list;
- secrets read directly by a pipeline from an external system, for example with the `withVault` step,
  are not Jenkins credentials: they are neither reported nor tracked.

The `usages` of a credential are complete only as far as Jenkins tracks them. Jenkins records a usage
only when the plugin consuming the credential records it, as Credentials Binding (`withCredentials`),
Git and SSH Agent do: a script reading a credential directly leaves no trace. Usage tracking can also
be turned off on the controller with the
`com.cloudbees.plugins.credentials.CredentialsProvider.fingerprintEnabled` system property.

### Folder Depth

The folder tree is walked down to 20 levels. A folder at the 20th level is reported, but the source
does not enter it, and logs a warning naming the folder. The limit protects the sync from anomalous
structures, such as a misconfigured proxy returning a folder as a child of itself: every level costs
one request. Real controllers usually have 2 to 5 levels, and an organization folder with its
multibranch projects only takes 2, because the branches are not walked.

### One Controller per Catalog

The data carry the Jenkins names, such as the `fullName` of a job or the id of a credential, without a
reference to the controller. Two controllers writing to the same Catalog produce colliding items when
they hold jobs, credentials, libraries or plugins with the same names.
