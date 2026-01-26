// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package azuredevops

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConnection(t *testing.T) {
	t.Parallel()

	cfg := config{
		OrganizationURL: "https://localhost:3000/myorg/",
		PersonalToken:   "pat",
	}

	conn := cfg.connection()
	assert.NotNil(t, conn)
	assert.Equal(t, "https://localhost:3000/myorg", conn.BaseUrl)
	assert.Equal(t, "Basic "+base64.StdEncoding.EncodeToString([]byte(":pat")), conn.AuthorizationString)
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
