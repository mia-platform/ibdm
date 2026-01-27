// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package azuredevops

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConnection(t *testing.T) {
	t.Parallel()

	cfg := config{
		OrganizationURL: "https://localhost:3000/myorg/",
		PersonalToken:   "pat",
	}

	client, err := cfg.client()
	require.NoError(t, err)
	assert.NotNil(t, client)
	assert.Equal(t, "https://localhost:3000/myorg/", client.organizationURL.String())
	assert.Equal(t, "pat", client.personalToken)
}

func TestValidation(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		config    config
		expectErr error
	}{
		"valid config": {
			config: config{
				OrganizationURL: "https://dev.azure.com/myorg/",
				PersonalToken:   "pat",
			},
		},
		"missing organization URL": {
			config: config{
				PersonalToken: "pat",
			},
			expectErr: ErrMissingEnvVariable,
		},
		"missing personal token": {
			config: config{
				OrganizationURL: "https://dev.azure.com/myorg/",
			},
			expectErr: ErrMissingEnvVariable,
		},
	}

	for testName, test := range tests {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()
			err := test.config.validate()
			if test.expectErr != nil {
				assert.ErrorIs(t, err, test.expectErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
