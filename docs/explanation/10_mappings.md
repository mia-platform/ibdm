# Mappings

Mappings in `ibdm` rely on the [Go Text Template] implementation from the `text/template` package.
The standard library handles parsing, validation, and rendering, while we focus on helper functions
that generate or reshape data on top of the default template functions.

Earlier versions of the tool only copied values from one structure to another.
That approach prevented more advanced data manipulation, so the current design explicitly embraces
templating to give authors the flexibility they need.

As an additional safeguard, missing key paths in the source data trigger errors.  
Without the original value we could emit `<no value>`, but that placeholder gives no clue about the
expected data type and makes recovery fragile.

## Identifier Template

Each mapped resource needs a unique identifier so that insert, update, and delete operations can
target the correct record.

To keep identifiers predictable, we evaluate them through a dedicated template.
This template exposes the same functions as the rest of the mapping, yet its output is always cast
to a string and validated against these rules:

- Length must be between 1 and 253 characters (inclusive).
- Only lowercase alphanumeric characters, `-`, or `.` are allowed.
- The first character must be a lowercase alphanumeric character.
- The final character must be a lowercase alphanumeric character.

## Metadata Templates

Metadata templates cover the fields required to populate the `metadata` section that will be sent to
the Mia-Platform Catalog.  
The list of the allowed `metadata` that can be set is:
`annotations`, `creationTimestamp`, `description`, `labels`, `links`, `name`, `tags`, `title`, `uid`.  
Any extra not allowed `metadata` will be ignored and not sent to the catalog.

We restrict template keys to a flat structure and rely on the template engine to build any nested
data inside the values, but we suggest to do it only if necessary and try to keep the structure
as flat as possible.  
This constraint keeps the catalog representation concise and makes searching for keys easier.

All templates are gathered into a YAML document before rendering.  
The template engine then produces the final YAML, which we convert into a dynamic structure for the
Mia-Platform Catalog.
Because YAML is a superset of JSON, any value you emit must be valid YAML (and may also be valid
JSON).

## Spec Templates

Spec templates cover every other field required to populate the `spec` section that will be sent to
the Mia-Platform Catalog.

We restrict template keys to a flat structure and rely on the template engine to build any nested
data inside the values, but we suggest to do it only if necessary and try to keep the structure
as flat as possible.  
This constraint keeps the catalog representation concise and makes searching for keys easier.

All templates are gathered into a YAML document before rendering.  
The template engine then produces the final YAML, which we convert into a dynamic structure for the
Mia-Platform Catalog.
Because YAML is a superset of JSON, any value you emit must be valid YAML (and may also be valid
JSON).

## Extra Templates

Extra templates cover the `extra` section that is meant to create, for each mapping, extra items.

A more comprehensive documentation can be found in [Extra Mappings](./20_extra_mappings.md).

## Fan-out

Several mappings can share the same `type`.
Every payload the source emits for that `type` is then rendered by each of those mappings, and each
mapping produces its own item, with its own `apiVersion`, `itemFamily`, templates and extra items.
Deletes fan out the same way: every mapping of the `type` sends its own delete.

Mappings sharing a `type` render in the order their files are loaded: the order of the
`--mapping-file` flags and, within a directory, the lexical order of the file names. The order is
therefore the same at every run. A mapping that fails on a payload, because a template errors or the Catalog rejects
the item, logs the error and does not prevent the other mappings from rendering it.
Every mapping renders its own copy of the payload, so a template that writes into its input, for
example with [`set`](../reference/10_mappings.md#set), does not affect the other mappings.

Each mapping sends its own items to the Mia-Platform Catalog, so N mappings on a `type` mean N
writes for every payload of that `type`.

### Which Mappings Receive a Payload

Three mechanisms decide which of the mappings sharing a `type` render a payload:

- **Fan-out**: by default, every mapping of the `type` receives every payload.
- **Source targeting**: when mappings sharing a `type` ask the source for different data, the source
	addresses each payload only to the mappings it was fetched for, identifying them by their
	[`name`](../reference/10_mappings.md#name). The mappings of a `type` that all ask for the same data
	keep receiving every payload.
- **`createIf`**: a mapping can decline a payload on upsert with a [`createIf`](../reference/10_mappings.md#createif) guard, evaluated against the payload. Source targeting runs first, so a guard only sees payloads meant for its mapping.

Source targeting applies to these sources:

- **Azure**, event stream: the resource is fetched once per distinct `extra.apiVersion` among the
	mappings of its `type`, and every mapping receives the payload fetched with its own `apiVersion`.
	A mapping without `apiVersion` receives no event, as before. The sync is unaffected, because the
	Resource Graph queries it runs take no `apiVersion`.
- **GitHub**: repositories are listed once per distinct `extra.apiVersion` among the repository
	mappings, and each listing reaches the mappings of its version. Workflow runs are fetched once per
	distinct `extra.apiVersion` among the workflow run mappings. Repository and push webhooks fetch the
	repository languages once per version.
- **Azure DevOps**: a webhook event reaches only the mappings whose `extra.eventNames` list it, and every `type` with such a mapping receives it.

Every other source sends every payload to every mapping of its `type`.

Every distinct `apiVersion` costs one more fetch: for the GitHub source, one more listing of every
repository of the organization.

### Mappings That Need Different Data

Fan-out suits mappings that are different renderings of the same payload.
When two mappings need data the source must fetch or select differently, and the difference is not
one the source targets on, they should not share a `type`.
Give each of them its own `type` and let the source emit that `type` itself: this is a change to
the source, not to the mappings.

The Azure resource sub-types are an example: the source emits the `functionapps` type, next to
`Microsoft.Web/sites`, for the App Service sites whose kind marks them as function apps, as the
[Azure how-to](../how-to/030_azure-source.md#resource-sub-types) describes.

When the difference lies only in the payload the mappings already receive, a `createIf` guard on
each mapping is enough.

### Colliding Items

No uniqueness is enforced on `type`, nor on the `apiVersion` and `itemFamily` of the mappings.
When two mappings produce items with the same `apiVersion`, `itemFamily` and identifier, they write
the same Catalog item, and the last mapping to write it wins.
Avoiding such collisions is the responsibility of whoever writes the mappings.

## Template Functions

The [Mappings Reference](../reference/10_mappings.md) lists the helper functions you can call inside
your templates to transform data for the target custom resource.
We intentionally expose a compact, practical set of utilities instead of a broad API surface.

Every exported helper follows secure, modern best practices so you can avoid common pitfalls when
shaping data for your mappings.

[Go Text Template]: https://pkg.go.dev/text/template "data-driven templates for generating textual output"
