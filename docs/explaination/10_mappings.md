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
- Only alphanumeric characters, `-`, or `.` are allowed.
- The first character must be alphanumeric.
- The final character must be alphanumeric.

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

## Template Functions

The [Mappings Reference](../reference/mappings.md) lists the helper functions you can call inside
your templates to transform data for the target custom resource.
We intentionally expose a compact, practical set of utilities instead of a broad API surface.

Every exported helper follows secure, modern best practices so you can avoid common pitfalls when
shaping data for your mappings.

[Go Text Template]: https://pkg.go.dev/text/template "data-driven templates for generating textual output"
