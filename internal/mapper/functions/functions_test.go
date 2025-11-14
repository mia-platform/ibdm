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
}

func TestStringsFunctions(t *testing.T) {
	t.Parallel()

	t.Run("trim function", func(t *testing.T) {
		t.Parallel()
		input := "   Hello, World!   "
		expected := "Hello, World!"
		assert.Equal(t, expected, TrimSpace(input))
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
