# Mappings

Mappings are essentially Go Text Templates that can return JSON compatible types.  
Their syntax **MUST** be a valid [Go Text Template] and you can see the available syntax
in the official documentation.

The only limitation that is imposed is that the template used for the `identifier` will be always
casted as a string.

## Additional Functions

In addition to the [default functions] available to all Go Text Templates we give you access to
these other ones:

- [trim](#trim)
- [upper](#upper)
- [lower](#lower)
- [toJSON](#tojson)
- [now](#now)
- [sha256sum](#sha256sum)
- [sha512sum](#sha512sum)
- [uuidv4](#uuidv4)
- [uuidv6](#uuidv6)
- [uuidv7](#uuidv7)

### `trim`

The `trim` function will remove all leading and trailing white space (as defined by Unicode) removed.

<!-- markdownlint-disable-next-line MD038 -->
As an example, `{{ .aKey | trim }}` with a `aKey` containing ` a complex string   ` will return
`a complex string`.

### `upper`

The `upper` function will change all Unicode letters mapped to their upper case.

As an example, `{{ .aKey | upper }}` with a `aKey` containing `hello world!` will return `HELLO WORLD!`.

### `lower`

The `lower` function will change all Unicode letters mapped to their lower case.

As an example, `{{ .aKey | lower }}` with a `aKey` containing `HELLO WORLD!` will return `hello world!`.

### `toJSON`

The `toJSON` function is used to transform complex data in JSON compatible form. This is needed
if you have to put objects or arrays inside one of your key in the mapper.

As an example, `{{ .aKey | toJSON }}` with a `aKey` containing an array of 5 number will return
`[ 1, 2, 3, 4, 5 ]`.

### `now`

The `now` function will return the actual timestamp when the mapping is occurring in UTC time zone
following the [RFC3339] format.

As an example `{{ now }}` will return `1969-07-20T20:17:40Z`.

### `sha256sum`

The `sha256sum` function will return the hash of the data passed to it in 256 bit form.

As an example `{{ .aKey | sha256sum }}` with a `aKey` containing `Hello World!` will return
`7f83b1657ff1fc53b92dc18148a1d65dfc2d4b1fa3d677284addd200126d9069`.

### `sha512sum`

The `sha512sum` function will return the hash of the data passed to it in 512 bit form.

As an example `{{ .aKey | sha256sum }}` with a `aKey` containing `Hello World!` will return
`861844d6704e8573fec34d967e20bcfef3d424cf48be04e6dc08f2bd58c729743371015ead891cc3cf1c9d34b49264b510751b1ff9e537937bc46b5d6ff4ecc8`.

### `uuidv4`

The `uuidv4` function will return a valid [UUID in the v4 format]. It's a valid default choice for
generating random UUIDs.

As an example `{{ uuidv4 }}` will return `2ab22cfe-6448-4ca3-bb5b-6d752461b0d1`.

### `uuidv6`

The `uuidv6` function will return a valid [UUID in the v6 format]. It's used as an alternative
to the old v1 format and can be used to maintain compatibility with it as an id in databases
where the v1 format has already been used.

As an example `{{ uuidv6 }}` will return `01f0c399-715a-66be-bf48-62e7f2a0bc55`.

### `uuidv7`

The `uuidv7` function will return a valid [UUID in the v7 format]. It's the preferred choice
for generating UUIDs for DB usage when you don't have prior data or UUIDs generated with the
v1 version.

As an example `{{ uuidv7 }}` will return `019a912f-de3b-724d-81ce-3a602ebf1a98`.

[Go Text Template]: https://pkg.go.dev/text/template@go1.25.4 "data-driven templates for generating textual output"
[default functions]: https://pkg.go.dev/text/template@go1.25.4#hdr-Functions
[RFC3339]: https://www.rfc-editor.org/rfc/rfc3339
[UUID in the v4 format]: https://www.rfc-editor.org/rfc/rfc9562#name-uuid-version-4
[UUID in the v6 format]: https://www.rfc-editor.org/rfc/rfc9562#name-uuid-version-6
[UUID in the v7 format]: https://www.rfc-editor.org/rfc/rfc9562#name-uuid-version-7
