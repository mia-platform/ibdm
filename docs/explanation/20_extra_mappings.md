# Extra Mappings

Extra mappings cover the `extra` section of the template that is meant to create, for each mapping,
extra items that must be created in the same acquisition instance of the parent item of the mapping.  
In this way these extra items will be created using the data originated from the parent item and
will be sent to the Mia-Platform Catalog along with it.

Each `extra` must have these mandatory fields:

- `apiVersion`: API version of the ITD of the extra mappings
- `itemFamily`: family of the extra mappings
- `deletePolicy`: used to manage deletion propagation of the extra upon item mapping deletion event, can be `none` or `cascade`
- `identifier`: the identier of the extra item that will be created

There is an additional non mandatory field configurable named `createIf`
that can be used to set up even a complex boolean expression, that can depend on the parent item data,
that allows to validate if the creation of the extra is needed or not.

Additional fields can be needed depending on the `itemFamily` of extra that is going to be used in the mapping.

The `createIf` of an extra gates that extra item only.
To let a whole mapping decline a payload, use the root [`createIf`](../reference/10_mappings.md#createif)
of the mapping instead: when it declines, none of the extra items of the mapping are created either.

## Extra Items and Fan-out

The extra items of a mapping are created by that mapping only, from the payloads that mapping
renders: when several mappings share a `type`, as described in [Fan-out](./10_mappings.md#fan-out),
each of them creates its own extra items.

The target of `mappings.extra` is deliberately many-to-one: many mappings write extra items to the same
item type definition. The bundled mappings, for example, create `mia-platform.eu/v1` `relationships`
items from 12 different mapping files.
Fan-out does not change this, and no uniqueness is enforced on the extra items either: two extra
items with the same `apiVersion`, `itemFamily` and identifier are the same Catalog item, and the last
one written wins.

We restrict template keys to a flat structure and rely on the template engine to build any nested
data inside the values, but we suggest to do it only if necessary and try to keep the structure
as flat as possible.  
This constraint keeps the catalog representation concise and makes searching for keys easier.

All templates are gathered into a YAML document before rendering.  
The template engine then produces the final YAML, which we convert into a dynamic structure for the
Mia-Platform Catalog.
Because YAML is a superset of JSON, any value you emit must be valid YAML (and may also be valid
JSON).

## List of Allowed Item Family Extra Mappings

### Relationship

The `Relationship` represents the item that put in relation the item of the mapping itself,
later referred as `targetRef`of the relationship,
with other items that can or cannot be present in the Mia-Platform Catalog.  
The typical structure of a `relationship` is comprehensive of the required fields plus:

- `sourceRef`: it is the source of the relationship
- `typeRef`: describes the type of relationship existing between the source and the target

The `targetRef` is automatically computed by the mapping using the `apiVersion`, `itemFamily` and `identifier`
from the root mapping.

Data integrity of the `relationship` needs to be managed by the mappings itself and their developers.  
In case of `sourceRef` with wrong `apiVersion`, `itemFamily` or `identifier`,
thus not referring to actual existing items or definition, the Mia-Platform Catalog will not throw error and
will map the sent relationship anyway.

#### Example of Relationship Structure

``` yaml
extra:
  - apiVersion: mia-platform.eu/v1
    itemFamily: relationships
    deletePolicy: "cascade"
    createIf: |-
      {{ $value := (get "value" .example-obj (object)) -}}
      {{- $otherValue := (get "otherValue" $value nil) -}}
      {{- if $otherValue -}}
        true
      {{- else -}}
        false
      {{- end }}
    identifier: |-
      {{ $value := (get "value" .example-obj (object)) -}}
      {{- $otherValue := (get "otherValue" $value nil) -}}
      {{- printf "relationship-%s-%s-example-type" $otherValue .directValue | sha256sum}}
    sourceRef:
      apiVersion: "mia-platform.eu/v1"
      family: "family-example"
      name:  |-
        {{- printf "family-example-%s-example" .anotherId | sha256sum}}  
    typeRef:
      apiVersion: "mia-platform.eu/v1"
      family: "relationship-types"
      name: "example-type.mia-platform.eu"
```
