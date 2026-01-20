# Mappings

Mappings are Go text templates that can emit JSON-compatible values. Every mapping must use valid
[Go Text Template] syntax.
Refer to the official documentation for language rules and control structures.

The only enforced limitation is that any template used as an `identifier` is always cast to a string.

## Additional Functions

Alongside the [default functions] available in Go templates, the mapper runtime exposes these
helper groups:

- Strings:
	- [quote](#quote)
	- [trim](#trim)
	- [trimPrefix](#trimprefix)
	- [trimSuffix](#trimsuffix)
	- [replace](#replace)
	- [upper](#upper)
	- [lower](#lower)
	- [truncate](#truncate)
	- [split](#split)
	- [encode64](#encode64)
	- [decode64](#decode64)
- Objects:
	- [object](#object)
	- [toJSON](#tojson)
	- [pick](#pick)
	- [get](#get)
	- [set](#set)
- Lists:
	- [list](#list)
	- [append](#append)
	- [prepend](#prepend)
	- [first](#first)
	- [last](#last)
- Time:
	- [now](#now)
- Crypto:
	- [sha256sum](#sha256sum)
	- [sha512sum](#sha512sum)
- UUID:
	- [uuidv4](#uuidv4)
	- [uuidv6](#uuidv6)
	- [uuidv7](#uuidv7)

### `quote`

`quote` wraps the provided value in double quotes.

Example: `{{ .aKey | quote }}` with `.aKey` equal to `some data` produces `"some data"`.
Use it to guarantee string output even when the original value may be empty or of another type.

### `trim`

`trim` removes leading and trailing Unicode whitespace.

<!-- markdownlint-disable-next-line MD038 -->
Example: `{{ .aKey | trim }}` with `.aKey` equal to the string ` a complex string   ` produces
`a complex string`.

### `trimPrefix`

`trimPrefix` removes the provided substring from the beginning of the input string.

Example: `{{ .aKey | trimPrefix "prefix_" }}` with `.aKey` equal to `prefix_value` produces `value`.

### `trimSuffix`

`trimSuffix` removes the provided substring from the end of the input string.

Example: `{{ .aKey | trimSuffix ".txt" }}` with `.aKey` equal to `report.txt` produces `report`.

### `replace`

`replace` substitutes every non-overlapping occurrence of the first argument with the second argument.

Example: `{{ .aKey | replace "_" "-" }}` with `.aKey` equal to `hello_world` produces `hello-world`.

### `upper`

`upper` converts all Unicode letters in the input to upper case.

Example: `{{ .aKey | upper }}` with `.aKey` equal to `hello world!` produces `HELLO WORLD!`.

### `lower`

`lower` converts all Unicode letters in the input to lower case.

Example: `{{ .aKey | lower }}` with `.aKey` equal to `HELLO WORLD!` produces `hello world!`.

### `truncate`

`truncate` shortens the input string from the start or the end.
A positive index removes characters from the beginning, while a negative index removes characters
from the end. When the absolute value of the index is greater than the string length, the
original value is returned.

Examples:

- `{{ .aKey | truncate 5 }}` with `.aKey` equal to `Hello World!` produces `World!`.
- `{{ .aKey | truncate -7 }}` with the same input produces `Hello`.

### `split`

`split` separates a string on the provided separator and returns the resulting list.

Example: `{{ .aKey | split "/" }}` with `.aKey` equal to `hello/world` produces `["hello", "world"]`.

### `encode64`

`encode64` returns the base64 encoding of the input string.

Example: `{{ .aKey | encode64 }}` with `.aKey` equal to `hello world!` produces `aGVsbG8gd29ybGQh`.

### `decode64`

`decode64` decodes a base64-encoded string.

Example: `{{ .aKey | decode64 }}` with `.aKey` equal to `aGVsbG8gd29ybGQh` produces `hello world!`.

### `object`

`object` builds a map from alternating key and value arguments.

Example: `{{ object "key" .aKey | toJSON }}` with `.aKey` equal to `hello world!` produces
`{"key":"hello world!"}`.

### `toJSON`

`toJSON` converts complex data to a JSON string.
Use it when you need to embed objects or arrays inside another value.

Example: `{{ .aKey | toJSON }}` with `.aKey` equal to the array `[1 2 3 4 5]` produces `[1,2,3,4,5]`.

### `pick`

`pick` returns a new object that contains only the requested keys.

Example: `{{ pick .object "key" | toJSON }}` with `.object` equal to
`{"key":"value1","other":"value2"}` produces `{"key":"value1"}`.

### `get`

`get` retrieves the value stored at the provided key, or returns the supplied default when the key
is missing.

Example: `{{ get "key" .object "defaultValue" }}` with `.object` equal to `{"key":"value1"}`
produces `value1`.

### `set`

`set` stores the provided value at the given key and returns the updated object.

Example: `{{ set "otherKey" .aKey .object | toJSON }}` with `.object` equal to `{"key":"value1"}`
and `.aKey` equal to `new value` produces `{"key":"value1","otherKey":"new value"}`.

### `list`

`list` creates a new array containing the provided elements.

Example: `{{ list 1 2 3 4 }}` produces `[1,2,3,4]`.

### `append`

`append` adds elements to the end of an existing list.
It returns an error if the first argument is not an array or slice.

Example: `{{ append .list 3 4 5 }}` with `.list` equal to `[1,2]` produces `[1,2,3,4,5]`.

### `prepend`

`prepend` adds elements to the beginning of an existing list.
It returns an error if the first argument is not an array or slice.

Example: `{{ prepend .list 3 4 5 }}` with `.list` equal to `[1,2]` produces `[3,4,5,1,2]`.

### `first`

`first` returns the first element of a list, array, or string.
It returns `nil` for empty inputs and an error for unsupported types.

Example: `{{ .items | first }}` with `.items` equal to `[1,2,3]` produces `1`.

### `last`

`last` returns the final element of a list, array, or string.
It returns `nil` for empty inputs and an error for unsupported types.

Example: `{{ .items | last }}` with `.items` equal to `[1,2,3]` produces `3`.

### `now`

`now` returns the current UTC timestamp formatted according to [RFC3339].

Example: `{{ now }}` might produce `1969-07-20T20:17:40Z`.

### `sha256sum`

`sha256sum` returns the SHA-256 hash of the provided data as a hexadecimal string.

Example: `{{ .aKey | sha256sum }}` with `.aKey` equal to `Hello World!` produces
`7f83b1657ff1fc53b92dc18148a1d65dfc2d4b1f...d9069`.

### `sha512sum`

`sha512sum` returns the SHA-512 hash of the provided data as a hexadecimal string.

Example: `{{ .aKey | sha512sum }}` with `.aKey` equal to `Hello World!` produces
`861844d6704e8573fec34d967e20bcfe...6ff4ecc8`.

### `uuidv4`

`uuidv4` returns a random [UUID in the v4 format], which is a sound default for generating
identifiers.

Example: `{{ uuidv4 }}` might produce `2ab22cfe-6448-4ca3-bb5b-6d752461b0d1`.

### `uuidv6`

`uuidv6` returns a [UUID in the v6 format], a time-ordered alternative to the legacy v1 format.
Use it when you need ordering compatibility with existing v1 identifiers.

Example: `{{ uuidv6 }}` might produce `01f0c399-715a-66be-bf48-62e7f2a0bc55`.

### `uuidv7`

`uuidv7` returns a [UUID in the v7 format], the recommended option for new, time-ordered identifiers.
Prefer it when you do not already store v1 values.

Example: `{{ uuidv7 }}` might produce `019a912f-de3b-724d-81ce-3a602ebf1a98`.

[Go Text Template]: https://pkg.go.dev/text/template@go1.25.4 "data-driven templates for generating textual output"
[default functions]: https://pkg.go.dev/text/template@go1.25.4#hdr-Functions
[RFC3339]: https://www.rfc-editor.org/rfc/rfc3339
[UUID in the v4 format]: https://www.rfc-editor.org/rfc/rfc9562#name-uuid-version-4
[UUID in the v6 format]: https://www.rfc-editor.org/rfc/rfc9562#name-uuid-version-6
[UUID in the v7 format]: https://www.rfc-editor.org/rfc/rfc9562#name-uuid-version-7
