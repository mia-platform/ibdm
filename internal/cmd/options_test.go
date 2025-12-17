// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package cmd

import (
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"

	fakedestination "github.com/mia-platform/ibdm/internal/destination/fake"
)

func TestExecuteEventStream(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		options       *options
		expectedError error
	}{
		"run without errors": {
			options: &options{
				integrationName: "fake",
				sourceGetter:    testSourceGetter(t),
			},
		},
		"return error if sourcegetter fails": {
			options: &options{
				integrationName: "fake",
				sourceGetter: func(string) (any, error) {
					return nil, assert.AnError
				},
			},
			expectedError: assert.AnError,
		},
		"reading mappers fails": {
			options: &options{
				integrationName: "fake",
				mappingPaths: []string{
					"non-existing-file.yaml",
				},
				sourceGetter: testSourceGetter(t),
			},
			expectedError: syscall.ENOENT,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := tc.options.executeEventStream(t.Context())
			assert.ErrorIs(t, err, tc.expectedError)
		})
	}
}

func TestExecuteSync(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		options       *options
		expectedError error
	}{
		"run without errors": {
			options: &options{
				integrationName: "fake",
				sourceGetter:    testSourceGetter(t),
			},
		},
		"return error if sourcegetter fails": {
			options: &options{
				integrationName: "fake",
				sourceGetter: func(string) (any, error) {
					return nil, assert.AnError
				},
			},
			expectedError: assert.AnError,
		},
		"reading mappers fails": {
			options: &options{
				integrationName: "fake",
				mappingPaths: []string{
					"non-existing-file.yaml",
				},
				sourceGetter: testSourceGetter(t),
			},
			expectedError: syscall.ENOENT,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			destination := fakedestination.NewFakeDestination(t)
			tc.options.destination = destination

			err := tc.options.executeSync(t.Context())
			assert.ErrorIs(t, err, tc.expectedError)
		})
	}
}
