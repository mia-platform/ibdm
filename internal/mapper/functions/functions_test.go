// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package functions

import (
	"bytes"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCryptoFunctions(t *testing.T) {
	t.Parallel()

	t.Run("sha256Sum function", func(t *testing.T) {
		t.Parallel()

		input := "hello world"
		expected := "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
		assert.Equal(t, expected, Sha256Sum(input))
	})

	t.Run("sha512Sum function", func(t *testing.T) {
		t.Parallel()

		input := "hello world"
		expected := "309ecc489c12d6eb4cc40f50c902f2b4d0ed77ee511a7c7a9bcd3ca86d4cd86f989dd35bc5ff499670da34255b45b0cfd830e81f605dcf7dc5542e93ae9cd76f"
		assert.Equal(t, expected, Sha512Sum(input))
	})
}

func TestListFunctions(t *testing.T) {
	t.Parallel()

	t.Run("list", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, []any{1, "two", 3.0, true}, List(1, "two", 3.0, true))
		var expectedEmpty []any
		assert.Equal(t, expectedEmpty, List())
	})

	t.Run("append function", func(t *testing.T) {
		t.Parallel()

		original := []any{1, 2, 3}
		appended, err := Append(original, 4, 5)
		require.NoError(t, err)
		assert.Equal(t, []any{1, 2, 3, 4, 5}, appended)
	})

	t.Run("append function with unsupported type", func(t *testing.T) {
		t.Parallel()

		_, err := Append(123, 4, 5)
		require.Error(t, err)
	})

	t.Run("prepend function", func(t *testing.T) {
		t.Parallel()

		original := []any{3, 4, 5}
		prepended, err := Prepend(original, 1, 2)
		require.NoError(t, err)
		assert.Equal(t, []any{1, 2, 3, 4, 5}, prepended)
	})

	t.Run("prepend function with unsupported type", func(t *testing.T) {
		t.Parallel()

		_, err := Prepend(123, 4, 5)
		require.Error(t, err)
	})

	t.Run("first function", func(t *testing.T) {
		t.Parallel()

		list := []any{1, 2, 3, 4, 5}
		first, err := First(list)
		require.NoError(t, err)
		assert.Equal(t, 1, first)

		emptyList := []any{}
		firstEmpty, err := First(emptyList)
		require.NoError(t, err)
		assert.Nil(t, firstEmpty)
	})

	t.Run("first function with string", func(t *testing.T) {
		t.Parallel()

		str := "hello"
		first, err := First(str)
		require.NoError(t, err)
		assert.Equal(t, "h", first)

		emptyString := ""
		firstEmpty, err := First(emptyString)
		require.NoError(t, err)
		assert.Nil(t, firstEmpty)
	})

	t.Run("fist function with unsupported type", func(t *testing.T) {
		t.Parallel()

		value, err := First(123)
		require.Error(t, err)
		require.Nil(t, value)
	})

	t.Run("last function", func(t *testing.T) {
		t.Parallel()

		list := []any{1, 2, 3, 4, 5}
		last, err := Last(list)
		require.NoError(t, err)
		assert.Equal(t, 5, last)

		emptyList := []any{}
		lastEmpty, err := Last(emptyList)
		require.NoError(t, err)
		assert.Nil(t, lastEmpty)
	})

	t.Run("last function with string", func(t *testing.T) {
		t.Parallel()

		str := "hello"
		last, err := Last(str)
		require.NoError(t, err)
		assert.Equal(t, "o", last)

		emptyString := ""
		lastEmpty, err := Last(emptyString)
		require.NoError(t, err)
		assert.Nil(t, lastEmpty)
	})

	t.Run("last function with unsupported type", func(t *testing.T) {
		t.Parallel()

		value, err := Last(123)
		require.Error(t, err)
		require.Nil(t, value)
	})
}

func TestObjectsFunctions(t *testing.T) {
	t.Parallel()

	t.Run("object function", func(t *testing.T) {
		t.Parallel()

		expected := map[string]any{
			"name":    "Alice",
			"age":     30,
			"country": "Wonderland",
			"score":   nil,
		}
		assert.Equal(t, expected, Object("name", "Alice", "age", 30, "country", "Wonderland", "score"))
	})

	t.Run("toJSON function", func(t *testing.T) {
		t.Parallel()

		input := map[string]any{
			"name": "Alice",
			"age":  30,
		}
		expected := `{"age":30,"name":"Alice"}`
		assert.Equal(t, expected, ToJSON(input))
	})

	t.Run("pick function", func(t *testing.T) {
		t.Parallel()

		object := map[string]any{
			"name":    "Alice",
			"age":     30,
			"country": "Wonderland",
		}

		expected := map[string]any{"name": "Alice", "country": "Wonderland"}
		assert.Equal(t, expected, Pick(object, "name", "country"))
		assert.Equal(t, make(map[string]any), Pick(object, "nonexistent", "nonexistent2"))
	})

	t.Run("get function", func(t *testing.T) {
		t.Parallel()

		object := map[string]any{
			"name": "Alice",
			"age":  30,
		}
		expectedName := "Alice"
		expectedAge := 30
		assert.Equal(t, expectedName, Get("name", object, ""))
		assert.Equal(t, expectedAge, Get("age", object, nil))
		assert.Equal(t, true, Get("missing", object, true))
	})

	t.Run("set function", func(t *testing.T) {
		t.Parallel()

		object := map[string]any{
			"name": "Alice",
			"age":  30,
		}
		updatedObject := Set("country", "Wonderland", object)
		expected := map[string]any{
			"name":    "Alice",
			"age":     30,
			"country": "Wonderland",
		}
		assert.Equal(t, expected, updatedObject)
	})
}

func TestStringsFunctions(t *testing.T) {
	t.Parallel()

	t.Run("quote function", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, `"Hello, \"World\"!"`, Quote(`Hello, "World"!`))
		assert.Equal(t, `"123"`, Quote(123))
		assert.Equal(t, `"true"`, Quote(true))
		assert.Equal(t, `"3.14"`, Quote(3.14))
		assert.Equal(t, `"<nil>"`, Quote(nil))
		obj := map[string]any{"key": "value"}
		assert.Equal(t, `"map[key:value]"`, Quote(obj))
		buffer := new(bytes.Buffer)
		buffer.WriteString("byte slice")
		assert.Equal(t, `"byte slice"`, Quote(buffer))
	})

	t.Run("trim function", func(t *testing.T) {
		t.Parallel()
		input := "   Hello, World!   "
		expected := "Hello, World!"
		assert.Equal(t, expected, TrimSpace(input))
	})

	t.Run("trimPrefix function", func(t *testing.T) {
		t.Parallel()
		input := "Hello, World!"
		prefix := "Hello, "
		expected := "World!"
		assert.Equal(t, expected, TrimPrefix(prefix, input))
	})

	t.Run("trimSuffix function", func(t *testing.T) {
		t.Parallel()
		input := "Hello, World!"
		suffix := " World!"
		expected := "Hello,"
		assert.Equal(t, expected, TrimSuffix(suffix, input))
	})

	t.Run("replace function", func(t *testing.T) {
		t.Parallel()
		input := "Hello, World! World!"
		toChange := "World"
		toBe := "Universe"
		expected := "Hello, Universe! Universe!"
		assert.Equal(t, expected, Replace(toChange, toBe, input))
	})

	t.Run("toUpper function", func(t *testing.T) {
		t.Parallel()
		input := "hello, world!"
		expected := "HELLO, WORLD!"
		assert.Equal(t, expected, ToUpper(input))
	})

	t.Run("toLower function", func(t *testing.T) {
		t.Parallel()
		input := "HELLO, WORLD!"
		expected := "hello, world!"
		assert.Equal(t, expected, ToLower(input))
	})

	t.Run("truncate function", func(t *testing.T) {
		t.Parallel()
		input := "Hello, World!"

		assert.Equal(t, "Hello", Truncate(5, input))
		assert.Equal(t, "World!", Truncate(-6, input))
		assert.Equal(t, "Hello, World!", Truncate(20, input))
		assert.Equal(t, "Hello, World!", Truncate(-20, input))
	})

	t.Run("split function", func(t *testing.T) {
		t.Parallel()
		input := "apple,banana,cherry"
		sep := ","
		expected := []string{"apple", "banana", "cherry"}
		assert.Equal(t, expected, Split(sep, input))
	})

	t.Run("encodeBase64 function", func(t *testing.T) {
		t.Parallel()
		input := "hello world"
		expected := "aGVsbG8gd29ybGQ="
		assert.Equal(t, expected, EncodeBase64(input))
	})

	t.Run("decodeBase64", func(t *testing.T) {
		t.Parallel()
		input := "aGVsbG8gd29ybGQ="
		expected := "hello world"
		result, err := DecodeBase64(input)
		require.NoError(t, err)
		assert.Equal(t, expected, result)
	})
}

func TestDateFunctions(t *testing.T) {
	t.Parallel()

	t.Run("now function", func(t *testing.T) {
		t.Parallel()
		loc := time.FixedZone("Fixed+01", int((1 * time.Hour).Seconds()))
		nowFn = func() time.Time {
			return time.Date(2024, 6, 10, 15, 4, 5, 0, loc)
		}

		expected := "2024-06-10T14:04:05Z"
		assert.Equal(t, expected, Now())
	})
}

func TestUUIDFunctions(t *testing.T) {
	t.Parallel()

	t.Run("UUIDV4 function", func(t *testing.T) {
		t.Parallel()
		id, err := UUIDV4()
		require.NoError(t, err)

		parsedID, err := uuid.Parse(id)
		require.NoError(t, err)
		assert.EqualValues(t, 4, parsedID.Version())
	})

	t.Run("UUIDV6 function", func(t *testing.T) {
		t.Parallel()
		id, err := UUIDV6()
		require.NoError(t, err)

		parsedID, err := uuid.Parse(id)
		require.NoError(t, err)
		assert.EqualValues(t, 6, parsedID.Version())
	})

	t.Run("UUIDV7 function", func(t *testing.T) {
		t.Parallel()
		id, err := UUIDV7()
		require.NoError(t, err)

		parsedID, err := uuid.Parse(id)
		require.NoError(t, err)
		assert.EqualValues(t, 7, parsedID.Version())
	})
}
