// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewConsoleService(t *testing.T) {
	t.Run("fails when CONSOLE_ENDPOINT is missing", func(t *testing.T) {
		t.Setenv("CONSOLE_ENDPOINT", "")
		svc, err := NewConsoleService()
		require.Error(t, err)
		require.Nil(t, svc)
	})

	t.Run("fails when credentials are incomplete", func(t *testing.T) {
		t.Setenv("CONSOLE_ENDPOINT", "http://example.com")
		t.Setenv("CONSOLE_CLIENT_ID", "foo")
		t.Setenv("CONSOLE_CLIENT_SECRET", "") // Missing secret
		svc, err := NewConsoleService()
		require.Error(t, err)
		require.Nil(t, svc)
		require.Contains(t, err.Error(), errMissingClientSecret.Error())
	})

	t.Run("succeeds with valid config", func(t *testing.T) {
		t.Setenv("CONSOLE_ENDPOINT", "http://example.com")
		svc, err := NewConsoleService()
		require.NoError(t, err)
		require.NotNil(t, svc)
		require.Equal(t, "http://example.com", svc.ConsoleEndpoint)
	})

	t.Run("infers AuthEndpoint from ConsoleEndpoint", func(t *testing.T) {
		t.Setenv("CONSOLE_ENDPOINT", "http://example.com/api/v1")
		svc, err := NewConsoleService()
		require.NoError(t, err)
		require.Equal(t, "http://example.com/oauth/token", svc.AuthEndpoint)
	})
}

func TestDoRequest(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		statusCode    int
		responseBody  any
		expectedError string
	}{
		"success (204)": {
			statusCode: http.StatusNoContent,
		},
		"forbidden (403)": {
			statusCode:    http.StatusForbidden,
			expectedError: "invalid token or insufficient permissions",
		},
		"not found (404)": {
			statusCode:    http.StatusNotFound,
			expectedError: "integration registration not found",
		},
		"json error message": {
			statusCode:    http.StatusBadRequest,
			responseBody:  map[string]string{"message": "bad request details"},
			expectedError: "bad request details",
		},
		"unknown error": {
			statusCode:    http.StatusInternalServerError,
			responseBody:  "server exploded",
			expectedError: "unexpected error",
		},
	}

	for testName, test := range tests {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodGet, r.Method)
				require.Equal(t, "application/json", r.Header.Get("Accept"))

				// Validate User-Agent is set
				require.NotEmpty(t, r.Header.Get("User-Agent"))

				if test.statusCode != http.StatusNoContent {
					w.WriteHeader(test.statusCode)
					if test.responseBody != nil {
						if m, ok := test.responseBody.(map[string]string); ok {
							_ = json.NewEncoder(w).Encode(m)
						} else {
							_, _ = w.Write([]byte(test.responseBody.(string)))
						}
					}
				} else {
					w.WriteHeader(http.StatusNoContent)
				}
			}))
			defer server.Close()

			svc := &ConsoleService{
				config: config{
					ConsoleEndpoint: server.URL,
				},
			}

			_, err := svc.DoRequest(t.Context(), "configuration", "main")

			if test.expectedError != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), test.expectedError)

				// Verify it's a ConsoleError
				var consoleErr *ConsoleError
				require.ErrorAs(t, err, &consoleErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestDoRequest_ContextCanceled(t *testing.T) {
	t.Setenv("CONSOLE_ENDPOINT", "http://example.com")
	svc, err := NewConsoleService()
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err = svc.DoRequest(ctx, "configuration", "main")
	require.NoError(t, err)
}

func Test_RealCase(t *testing.T) {
	t.Skip("skipping real case test; uncomment to run against real Console instance")
	// Load configuration JSON
	cfgBytes, err := os.ReadFile("../../../local/secret/basic/config.json")
	require.NoError(t, err)

	var jc struct {
		ConsoleEndpoint string `json:"ConsoleEndpoint"`
		AuthEndpoint    string `json:"AuthEndpoint"`
		ClientID        string `json:"ClientID"`
		ClientSecret    string `json:"ClientSecret"`
	}
	require.NoError(t, json.Unmarshal(cfgBytes, &jc))

	svc := &ConsoleService{
		config: config{
			ConsoleEndpoint: jc.ConsoleEndpoint,
			AuthEndpoint:    jc.AuthEndpoint,
			ClientID:        jc.ClientID,
			ClientSecret:    jc.ClientSecret,
		},
	}

	require.NotEmpty(t, svc.ConsoleEndpoint)
	require.NotEmpty(t, svc.AuthEndpoint)
	require.NotEmpty(t, svc.ClientID)
	require.NotEmpty(t, svc.ClientSecret)

	_, err = svc.DoRequest(t.Context(), "configuration", "main")
	require.NoError(t, err)
}

func Test_RealCase_JWT(t *testing.T) {
	t.Skip("skipping real case test; uncomment to run against real Console instance")
	// Load JWT configuration JSON
	cfgBytes, err := os.ReadFile("../../../local/secret/jwt/config.json")
	require.NoError(t, err)

	var jc struct {
		ConsoleEndpoint string `json:"ConsoleEndpoint"`
		AuthEndpoint    string `json:"AuthEndpoint"`
		ClientID        string `json:"ClientID"`
		PrivateKey      string `json:"PrivateKey"`
		PrivateKeyID    string `json:"PrivateKeyID"`
	}
	require.NoError(t, json.Unmarshal(cfgBytes, &jc))

	// Load private key file
	keyBytes, err := os.ReadFile("../../../local/secret/jwt/private.key")
	require.NoError(t, err)
	if jc.PrivateKey == "" {
		jc.PrivateKey = string(keyBytes)
	}

	svc := &ConsoleService{
		config: config{
			ConsoleEndpoint:          jc.ConsoleEndpoint,
			AuthEndpoint:             jc.AuthEndpoint,
			ClientID:                 jc.ClientID,
			PrivateKey:               jc.PrivateKey,
			PrivateKeyID:             jc.PrivateKeyID,
			ConsoleJWTServiceAccount: true,
		},
	}

	require.NotEmpty(t, svc.ConsoleEndpoint)
	require.NotEmpty(t, svc.AuthEndpoint)
	require.NotEmpty(t, svc.ClientID)
	require.NotEmpty(t, svc.PrivateKey)
	require.NotEmpty(t, svc.PrivateKeyID)

	_, err = svc.DoRequest(t.Context(), "configuration", "main")
	require.NoError(t, err)
}
