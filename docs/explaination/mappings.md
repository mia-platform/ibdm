# Mappings

Mappings in `ibdm` are implemented with the [Go Text Template] of the `text/template` package.  
This allow us to use the power of the standard library to do the heavy lifting of parsing,
validating and generating the output and let us to create only utility functions for
generating and/or modifying the data augmenting the default functions available.

In the previous iteration of a similar tool we opted to use a simpler approach and allow only to
map data from a structure to a new one, without giving tools to do some more advanced manipulation
and it has been a limiting factor for some users.

As an additional safekeeping we treat missing key paths from the original data as errors to avoid
empty values that in some cases are rendered as `<no value>` and from wich we cannot infer what is
the original data type to substitute to it.

## Identifier Template

Every mapped data must have an unique identifier that will be used to identify it during insert,
update and delete operations.

To ensure this a seprate template is used from the ones used to fill out the other data.  
This template will have access to the same functions of the others one but ti will always cast
as string and it will also checked on some specific constrains that are:

- it must be long between 1 and 253 characters inclued
- can contain only alphanumeric characters, `-` or `.`
- must start with an alphanumeric character
- must end with an alphanumeric character

## Spec Templates

Spec Templates are the collection of all the other templates that are needed to populate the
`spec` data to send to the Mia-Platform Catalog.

We are limiting the possibility to add only flat keys as templates and leave the power of the
template engine for creating complex data structure as values.  
These limit is intentional for keeping the structure as lean as possible and simplify search and
retrival of the information from the catalog.

The templates will be aggregated in YAML object and then the created file will be feeded to the
template engine to create a YAML rappresentation of the data.  
From this rappresentetion we will generate a dynamic structure that will be later sent to the
Mia-Platform Catalog, so you can always keep in mind that the template must return valid YAML
values (and because YAML is a JSON superset you can also return JSON valid values).

## Template Functions

In the [Mappings Reference](../reference/mappings.md) you can find links and descriptions for
the functions that you can use in your template for generate and/or modify the data as you
see fit for your target CRD.  
We aim to provide a small surface of functions that can provide useful utility for the data
and not expose a great API surface to remember.

We also try to expose only functions that provide secure and current best practice to avoid
common pitfall in data modelling.

[Go Text Template]: https://pkg.go.dev/text/template "data-driven templates for generating textual output"
