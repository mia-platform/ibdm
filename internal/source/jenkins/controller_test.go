// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package jenkins

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestControllerKey(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		rawURL   string
		expected string
	}{
		"https host":                     {rawURL: "https://ci.example.com", expected: "ci.example.com"},
		"http host":                      {rawURL: "http://ci.example.com", expected: "ci.example.com"},
		"uppercase scheme":               {rawURL: "HTTPS://ci.example.com", expected: "ci.example.com"},
		"mixed case host":                {rawURL: "https://CI.Example.com", expected: "ci.example.com"},
		"https default port":             {rawURL: "https://ci.example.com:443", expected: "ci.example.com"},
		"http default port":              {rawURL: "http://ci.example.com:80", expected: "ci.example.com"},
		"https with the http port":       {rawURL: "https://ci.example.com:80", expected: "ci.example.com:80"},
		"http with the https port":       {rawURL: "http://ci.example.com:443", expected: "ci.example.com:443"},
		"https non default port":         {rawURL: "https://ci.example.com:8443", expected: "ci.example.com:8443"},
		"http non default port":          {rawURL: "http://ci.example.com:8080", expected: "ci.example.com:8080"},
		"empty port":                     {rawURL: "https://ci.example.com:", expected: "ci.example.com"},
		"path":                           {rawURL: "https://ci.example.com/jenkins", expected: "ci.example.com/jenkins"},
		"other path":                     {rawURL: "https://ci.example.com/other", expected: "ci.example.com/other"},
		"path case is kept":              {rawURL: "https://ci.example.com/Jenkins", expected: "ci.example.com/Jenkins"},
		"nested path":                    {rawURL: "https://ci.example.com/ci/jenkins", expected: "ci.example.com/ci/jenkins"},
		"escaped path":                   {rawURL: "https://ci.example.com/my%20jenkins", expected: "ci.example.com/my%20jenkins"},
		"trailing slash":                 {rawURL: "https://ci.example.com/", expected: "ci.example.com"},
		"path with trailing slash":       {rawURL: "https://ci.example.com/jenkins/", expected: "ci.example.com/jenkins"},
		"every normalisation":            {rawURL: "HTTPS://CI.Example.com:443/jenkins/", expected: "ci.example.com/jenkins"},
		"ip address with port":           {rawURL: "http://192.0.2.10:8080", expected: "192.0.2.10:8080"},
		"ipv6 address":                   {rawURL: "https://[2001:DB8::1]", expected: "[2001:db8::1]"},
		"ipv6 address with default port": {rawURL: "https://[2001:db8::1]:443/jenkins", expected: "[2001:db8::1]/jenkins"},
		"ipv6 address with port":         {rawURL: "http://[2001:db8::1]:8080", expected: "[2001:db8::1]:8080"},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			key, err := controllerKey(tc.rawURL)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, key)
		})
	}
}

func TestControllerKeyInvalidURL(t *testing.T) {
	t.Parallel()

	_, err := controllerKey("https://ci.example.com/%zz")
	require.ErrorIs(t, err, ErrInvalidEnvVariable)
}

func TestNewControllerRef(t *testing.T) {
	t.Parallel()

	controller, err := newControllerRef("HTTPS://CI.Example.com:443/jenkins/")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{
		keyControllerKey: "ci.example.com/jenkins",
		keyURL:           "HTTPS://CI.Example.com:443/jenkins",
	}, controller.values(), "the url is the configured one without trailing slash")
}
