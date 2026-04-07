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
  - apiVersion: mia-platform.eu/v1alpha1
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
      apiVersion: "mia-platform.eu/v1alpha1"
      family: "family-example"
      name:  |-
        {{- printf "family-example-%s-example" .anotherId | sha256sum}}  
    typeRef:
      apiVersion: "mia-platform.eu/v1alpha1"
      family: "relationship-types"
      name: "example-type.mia-platform.eu"
```
