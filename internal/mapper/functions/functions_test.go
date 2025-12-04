// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package functions

import (
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

func TestObjectsFunctions(t *testing.T) {
	t.Parallel()

	t.Run("toJSON function", func(t *testing.T) {
		t.Parallel()

		input := map[string]any{
			"name": "Alice",
			"age":  30,
		}
		expected := `{"age":30,"name":"Alice"}`
		assert.Equal(t, expected, ToJSON(input))
	})

	t.Run("pluck function", func(t *testing.T) {
		t.Parallel()

		objects := []map[string]any{
			{"id": 1, "value": "Alice"},
			{"id": 2, "value": 1},
			{"id": 3, "value": "Charlie"},
		}
		expected := []any{"Alice", 1, "Charlie"}
		assert.Equal(t, expected, Pluck("value", objects))
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
}

func TestStringsFunctions(t *testing.T) {
	t.Parallel()

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
