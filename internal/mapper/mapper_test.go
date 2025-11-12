// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package mapper

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMapper(t *testing.T) {
	t.Parallel()

	t.Run("new mapper from valid templates", func(t *testing.T) {
		t.Parallel()
		mapper, err := New("{{ .name }}", map[string]string{
			"key":      "name",
			"otherKey": "{{ .otherKey | trim }}",
		})
		assert.NoError(t, err)
		assert.NotNil(t, mapper)

		mapperInstance, ok := mapper.(*internalMapper)
		require.True(t, ok)
		require.NotNil(t, mapperInstance.idTemplate)
		require.Len(t, mapperInstance.templates, 2)
		require.NotNil(t, mapperInstance.templates["key"])
		require.NotNil(t, mapperInstance.templates["otherKey"])
	})

	t.Run("return error when one template is broken", func(t *testing.T) {
		t.Parallel()
		mapper, err := New("{{ .name }}", map[string]string{
			"key":      "name",
			"otherKey": "{{ .otherKey | unknwonFunc }}",
		})
		assert.Nil(t, mapper)
		assert.Error(t, err)
		assert.ErrorContains(t, err, errTemplateParsing)

		var targetError *ParsingError
		assert.ErrorAs(t, err, &targetError)
		joinedErrors, ok := targetError.Unwrap().(interface{ Unwrap() []error })
		require.True(t, ok)
		require.Len(t, joinedErrors.Unwrap(), 1)
	})

	t.Run("return error when one or more template is broken", func(t *testing.T) {
		t.Parallel()
		mapper, err := New("{{ .name | unknwonFunc }}", map[string]string{
			"key":      "name",
			"otherKey": "{{ .otherKey | unknwonFunc }}",
		})
		assert.Nil(t, mapper)
		assert.Error(t, err)
		assert.ErrorContains(t, err, errTemplateParsing)

		var targetError *ParsingError
		assert.ErrorAs(t, err, &targetError)
		joinedErrors, ok := targetError.Unwrap().(interface{ Unwrap() []error })
		require.True(t, ok)
		require.Len(t, joinedErrors.Unwrap(), 2)
	})
}

func TestMapper(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		mapper        Mapper
		input         map[string]any
		expected      map[string]any
		expectedError error
	}{}

	for testName, test := range testCases {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			output, err := test.mapper.ApplyTemplates(test.input)
			if test.expectedError != nil {
				assert.Nil(t, output)
				assert.ErrorIs(t, err, test.expectedError)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, test.expected, output)
		})
	}
}
