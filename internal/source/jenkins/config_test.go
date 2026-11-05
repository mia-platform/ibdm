// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package jenkins

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfigFromEnv(t *testing.T) {
	// No t.Parallel(): the test uses t.Setenv.
	validEnv := map[string]string{
		urlEnvName:      "https://ci.example.com",
		usernameEnvName: "my-user",
		apiTokenEnvName: "my-api-token",
	}

	testCases := map[string]struct {
		envVars        map[string]string
		expectedErr    error
		expectedMsg    string
		expectedConfig config
	}{
		"required variables set": {
			envVars: validEnv,
			expectedConfig: config{
				URL:         "https://ci.example.com",
				Username:    "my-user",
				APIToken:    "my-api-token",
				HTTPTimeout: 30 * time.Second,
			},
		},
		"url with path and trailing slash": {
			envVars: map[string]string{
				urlEnvName:      "http://ci.example.com:8080/jenkins/",
				usernameEnvName: "my-user",
				apiTokenEnvName: "my-api-token",
				timeoutEnvName:  "10s",
			},
			expectedConfig: config{
				URL:         "http://ci.example.com:8080/jenkins",
				Username:    "my-user",
				APIToken:    "my-api-token",
				HTTPTimeout: 10 * time.Second,
			},
		},
		"all missing": {
			envVars:     map[string]string{},
			expectedErr: ErrMissingEnvVariable,
			expectedMsg: "JENKINS_URL, JENKINS_USERNAME, JENKINS_API_TOKEN",
		},
		"missing token": {
			envVars:     map[string]string{urlEnvName: "https://ci.example.com", usernameEnvName: "my-user"},
			expectedErr: ErrMissingEnvVariable,
			expectedMsg: apiTokenEnvName,
		},
		"unsupported scheme": {
			envVars:     map[string]string{urlEnvName: "ftp://ci.example.com", usernameEnvName: "my-user", apiTokenEnvName: "my-api-token"},
			expectedErr: ErrInvalidEnvVariable,
			expectedMsg: "scheme",
		},
		"relative url": {
			envVars:     map[string]string{urlEnvName: "ci.example.com", usernameEnvName: "my-user", apiTokenEnvName: "my-api-token"},
			expectedErr: ErrInvalidEnvVariable,
		},
		"url without host": {
			envVars:     map[string]string{urlEnvName: "https:///jenkins", usernameEnvName: "my-user", apiTokenEnvName: "my-api-token"},
			expectedErr: ErrInvalidEnvVariable,
			expectedMsg: "host",
		},
		"url with user information": {
			envVars:     map[string]string{urlEnvName: "https://someone@ci.example.com", usernameEnvName: "my-user", apiTokenEnvName: "my-api-token"},
			expectedErr: ErrInvalidEnvVariable,
			expectedMsg: "user information",
		},
		"url with query": {
			envVars:     map[string]string{urlEnvName: "https://ci.example.com?a=b", usernameEnvName: "my-user", apiTokenEnvName: "my-api-token"},
			expectedErr: ErrInvalidEnvVariable,
			expectedMsg: "query",
		},
		"unparsable url": {
			envVars:     map[string]string{urlEnvName: "https://ci.example.com/%zz", usernameEnvName: "my-user", apiTokenEnvName: "my-api-token"},
			expectedErr: ErrInvalidEnvVariable,
		},
		"invalid timeout": {
			envVars:     map[string]string{urlEnvName: "https://ci.example.com", usernameEnvName: "my-user", apiTokenEnvName: "my-api-token", timeoutEnvName: "soon"},
			expectedErr: ErrInvalidEnvVariable,
		},
		"non positive timeout": {
			envVars:     map[string]string{urlEnvName: "https://ci.example.com", usernameEnvName: "my-user", apiTokenEnvName: "my-api-token", timeoutEnvName: "0s"},
			expectedErr: ErrInvalidEnvVariable,
			expectedMsg: timeoutEnvName,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			for _, key := range []string{urlEnvName, usernameEnvName, apiTokenEnvName, timeoutEnvName} {
				t.Setenv(key, "")
			}
			for key, value := range tc.envVars {
				t.Setenv(key, value)
			}

			cfg, err := loadConfigFromEnv()
			if tc.expectedErr != nil {
				require.ErrorIs(t, err, tc.expectedErr)
				assert.ErrorContains(t, err, tc.expectedMsg)
				assert.NotContains(t, err.Error(), "my-api-token")
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.expectedConfig, cfg)
		})
	}
}

func TestNewSource(t *testing.T) {
	// No t.Parallel(): the test uses t.Setenv.
	t.Run("valid configuration", func(t *testing.T) {
		t.Setenv(urlEnvName, "https://ci.example.com/")
		t.Setenv(usernameEnvName, "my-user")
		t.Setenv(apiTokenEnvName, "my-api-token")
		t.Setenv(timeoutEnvName, "")

		src, err := NewSource()
		require.NoError(t, err)
		require.NotNil(t, src)
		assert.Equal(t, "https://ci.example.com", src.client.baseURL)
		assert.Equal(t, "my-user", src.client.username)
		assert.Equal(t, "my-api-token", src.client.apiToken)
		assert.Equal(t, 30*time.Second, src.client.httpClient.Timeout)
	})

	t.Run("invalid configuration", func(t *testing.T) {
		t.Setenv(urlEnvName, "")
		t.Setenv(usernameEnvName, "")
		t.Setenv(apiTokenEnvName, "")

		src, err := NewSource()
		require.ErrorIs(t, err, ErrJenkinsSource)
		require.ErrorIs(t, err, ErrMissingEnvVariable)
		assert.Nil(t, src)
	})
}
