# Internal and External Mappings

`ibdm` renders the data of an integration into Mia-Platform Catalog items through **mappings**. A
run can use two kinds of them:

- **Internal mappings** are shipped inside the `ibdm` binary, one set per integration. They write the
  system item types of the Mia-Platform Catalog, and they need no file to deploy.
- **External mappings** are files you write and pass with `--mapping-file`. They write item types of
  your own.

## Internal Mappings

List the internal mappings of an integration with:

```sh
ibdm mappings list console
```

Each name can be used with the two selection flags:

- `--include-internal-mappings` chooses the internal mappings to use: a comma separated list of
  names, or `all` for every one of them.
- `--exclude-internal-mappings` removes names from the ones included.

| Command | Internal mappings used |
| --- | --- |
| `ibdm run console` | none: nothing happens |
| `ibdm run console --include-internal-mappings=all` | every internal mapping of `console` |
| `ibdm run console --include-internal-mappings=projects,services` | `projects` and `services` |
| `ibdm run console --include-internal-mappings=all --exclude-internal-mappings=revisions` | every one except `revisions` |
| `ibdm run console --include-internal-mappings=projects,services --exclude-internal-mappings=projects` | `services` |
| `ibdm run console --exclude-internal-mappings=revisions` | none, and a warning: exclusion alone selects nothing |

`run` and `sync` follow the same rules. When no mapping is selected at all, the command logs
`<integration>: no mappings selected, nothing to do` and exits without starting anything.

The following are errors:

- `all` mixed with other names, as in `--include-internal-mappings=all,projects`;
- `--exclude-internal-mappings=all`;
- a name that is not an internal mapping of the integration: the error lists the valid names;
- an empty element in a list, as in `projects,,services`.

A flag given with an empty value, as happens when `--include-internal-mappings=$MAPPINGS` runs with
`MAPPINGS` unset, selects nothing and logs a warning. Check the variable or template that produced it.
Before any work begins, `ibdm` logs the internal and external mappings the run uses.

### Selections Depend on Each Other

Some internal mappings only produce items while another type is being fetched. For example,
GitLab pipelines are only fetched while the projects are walked, so
`--include-internal-mappings=pipelines` alone produces nothing. Relationships between items can
also point at items of a mapping you did not select. The how-to page of each integration describes
its dependencies.

Excluding a mapping does not delete the items it already published: they stop being updated and
have to be removed by hand.

### Selection and `syncable`

`syncable` and the selection flags answer two different questions:

- `syncable: false` states a capability of the integration: a full sync cannot produce that mapping's
  items, so `sync` always skips it.
- The selection flags state what you want for this run.

Under `sync`, a non-syncable mapping reached through `all` is skipped and reported, while one named
explicitly in `--include-internal-mappings` is an error.

## External Mappings

External mappings are loaded from the files and directories passed with `--mapping-file`, which
can be repeated. They are **always loaded in full** and **added** to the selected internal mappings:
the selection flags never narrow them.

```sh
# external mappings only
ibdm sync console --mapping-file ./my-mappings/

# every internal mapping, plus the external ones
ibdm sync console --include-internal-mappings=all --mapping-file ./my-mappings/
```

The selection flags only know internal names. Passing the name of an external mapping to them is an
error that says so.

### Reserved Domains

An external mapping cannot write a system item type. It is refused when the group of its root
`apiVersion`, the part before the first `/`, is one of these domains, or a subdomain of one:

- `mia-platform.eu`
- `mia-care.io`
- `mia-fintech.io`

For example, `console.mia-platform.eu/v1` and `mia-platform.eu/v1` are refused. The error names the
file and, when an internal mapping of the same name exists, the `--include-internal-mappings` value to
use instead. `mia-platform-experimental.eu` is **not** reserved, and external mappings may write to it.

Only the root `apiVersion` is checked. An entry of `mappings.extra` may still create
`mia-platform.eu/v1` `relationships` items, because relationships are how items are linked.

### Names

Every mapping of a run, internal or external, needs a unique [`name`](../reference/10_mappings.md#name).
An external file named like a selected internal mapping, such as a copy of `projects.yaml`, stops the
start. Set a different `name:` in the file, rename the file, or leave the internal mapping out of the
selection.

### Mappings Sharing an Item Type

When two or more mappings of a run write the same item type, meaning the same `apiVersion` and
`itemFamily`, their items may overwrite each other whenever their identifiers overlap. `ibdm`
refuses to start in that case, and lists every shared item type with the mappings writing it.
Pass `--allow-shared-item-types` to start anyway: every shared item type is then logged as a
warning. One mapping type writing several item types is always allowed.

### External Mappings on an Internal Type

An external mapping can render the same data as an internal mapping by declaring the same `type`.
Both mappings then render every payload of that type, each into its own item: see
[Fan-out](../explanation/10_mappings.md#fan-out).

The root `extra` of a mapping tells the integration how to fetch its data, such as the Azure or
GitHub `apiVersion`, or the Azure DevOps `eventNames`. When your mapping declares a different
`extra` from the internal mapping of its type, the integration fetches the data a second time, the
way your mapping asks, and logs a warning when it starts. Reuse the internal mapping's `extra`
unless you need data fetched differently: see it with `ibdm mappings list` and the
[list of internal mappings](../reference/20_internal-mappings.md).

## Migrating From Mapping Files

Earlier versions of `ibdm` shipped their mappings as files under `docs/mappings/`, passed with
`--mapping-file`. Those files are now internal mappings, and passing a copy of one is refused because
of its reserved domain.

| Before | Now |
| --- | --- |
| `ibdm sync gitlab --mapping-file docs/mappings/gitlab/` | `ibdm sync gitlab --include-internal-mappings=all` |
| `ibdm sync gitlab --mapping-file docs/mappings/gitlab/projects.yaml` | `ibdm sync gitlab --include-internal-mappings=projects` |
| a modified copy of a shipped file | move it to your own domain and pass it with `--mapping-file`, or use the internal mapping |
| a mounted or baked-in mapping directory | not needed for the internal mappings: remove the mount |

The shipped mapping files are still published with every release as `default-itds-mappings.tar.gz`,
as a starting point for your own mappings.
