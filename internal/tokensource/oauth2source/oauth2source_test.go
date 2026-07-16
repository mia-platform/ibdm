// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package oauth2source

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jwt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

const (
	rsaKeyBits = 4096
	// clientID is the fictional client identifier used across the tests in this package.
	clientID = "jwt-bearer-client"
)

// generateTestKey creates a fresh RSA key pair and wraps it into a jwk.Key, to be used as
// fictional test material. It is never used outside of this test file.
func generateTestKey(t *testing.T) (*rsa.PrivateKey, jwk.Key) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, rsaKeyBits)
	require.NoError(t, err)

	jwkKey, err := jwk.Import(key)
	require.NoError(t, err)

	return key, jwkKey
}

// newHappyPathServer starts a test server that serves a valid OIDC discovery document and a
// valid token response with the given expiresIn (in seconds), together with two protected
// resources that require a bearer token. It returns the server together with atomic counters
// tracking how many times the discovery and token endpoints were hit.
func newHappyPathServer(t *testing.T, key *rsa.PrivateKey, expiresIn int) (*httptest.Server, *atomic.Int32, *atomic.Int32) {
	t.Helper()

	var discoveryHits, tokenHits atomic.Int32
	var testServer *httptest.Server

	testServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			discoveryHits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			err := json.NewEncoder(w).Encode(map[string]string{
				"issuer":         testServer.URL,
				"token_endpoint": testServer.URL + "/oauth/token",
			})
			require.NoError(t, err)
		case "/oauth/token":
			tokenHits.Add(1)
			require.NoError(t, r.ParseForm())
			assert.Equal(t, "client_credentials", r.FormValue("grant_type"))
			assert.Equal(t, "urn:ietf:params:oauth:client-assertion-type:jwt-bearer", r.FormValue("client_assertion_type"))
			assert.Equal(t, clientID, r.FormValue("client_id"))
			assert.Equal(t, "private_key_jwt", r.FormValue("token_endpoint_auth_method"))

			assertion := r.FormValue("client_assertion")
			require.NotEmpty(t, assertion)

			parsed, err := jwt.Parse([]byte(assertion), jwt.WithKey(jwa.RS256(), key.Public()))
			require.NoError(t, err)

			issuer, ok := parsed.Issuer()
			require.True(t, ok)
			assert.Equal(t, clientID, issuer)

			subject, ok := parsed.Subject()
			require.True(t, ok)
			assert.Equal(t, clientID, subject)

			audience, ok := parsed.Audience()
			require.True(t, ok)
			assert.Equal(t, []string{testServer.URL + "/oauth/token"}, audience)

			jti, ok := parsed.JwtID()
			require.True(t, ok)
			assert.NotEmpty(t, jti)

			w.Header().Set("Content-Type", "application/json")
			err = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "generated-jwt-bearer-token",
				"token_type":   "Bearer",
				"expires_in":   expiresIn,
			})
			require.NoError(t, err)
		case "/protected-a", "/protected-b":
			assert.Equal(t, "Bearer generated-jwt-bearer-token", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(testServer.Close)

	return testServer, &discoveryHits, &tokenHits
}

// newDiscoveryOnlyServer starts a test server that serves a valid OIDC discovery document
// pointing at "/oauth/token", delegating requests to that path to tokenHandler. It is used by
// tests that only need to exercise the token-exchange failure path, discovery having already
// succeeded.
func newDiscoveryOnlyServer(t *testing.T, tokenHandler http.HandlerFunc) *httptest.Server {
	t.Helper()

	var testServer *httptest.Server

	testServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			w.Header().Set("Content-Type", "application/json")
			err := json.NewEncoder(w).Encode(map[string]string{
				"issuer":         testServer.URL,
				"token_endpoint": testServer.URL + "/oauth/token",
			})
			require.NoError(t, err)
		case "/oauth/token":
			tokenHandler(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(testServer.Close)

	return testServer
}

func TestPrivateKeyFlow(t *testing.T) {
	t.Parallel()

	key, jwkKey := generateTestKey(t)

	testServer, discoveryHits, tokenHits := newHappyPathServer(t, key, 3600)

	source, err := NewSource(t.Context(), clientID, testServer.URL, "", "", "", jwkKey)
	require.NoError(t, err)

	client := &http.Client{
		Transport: &oauth2.Transport{
			Source: source,
		},
	}

	resp, err := client.Get(testServer.URL + "/protected-a")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	assert.Equal(t, int32(1), discoveryHits.Load())
	assert.Equal(t, int32(1), tokenHits.Load())
}

func TestNewSourceCustomDiscoveryPath(t *testing.T) {
	// Not parallel: mutates the process environment via t.Setenv.
	key, jwkKey := generateTestKey(t)

	const customDiscoveryPath = "custom/discovery/document"
	t.Setenv("OIDC_DISCOVERY_PATH", customDiscoveryPath)

	var testServer *httptest.Server
	testServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/" + customDiscoveryPath:
			w.Header().Set("Content-Type", "application/json")
			err := json.NewEncoder(w).Encode(map[string]string{
				"issuer":         testServer.URL,
				"token_endpoint": testServer.URL + "/oauth/token",
			})
			require.NoError(t, err)
		case "/oauth/token":
			require.NoError(t, r.ParseForm())
			parsed, err := jwt.Parse([]byte(r.FormValue("client_assertion")), jwt.WithKey(jwa.RS256(), key.Public()))
			require.NoError(t, err)
			audience, ok := parsed.Audience()
			require.True(t, ok)
			assert.Equal(t, []string{testServer.URL + "/oauth/token"}, audience)

			w.Header().Set("Content-Type", "application/json")
			err = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "generated-jwt-bearer-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
			require.NoError(t, err)
		case "/protected":
			assert.Equal(t, "Bearer generated-jwt-bearer-token", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(testServer.Close)

	source, err := NewSource(t.Context(), clientID, testServer.URL, "", "", "", jwkKey)
	require.NoError(t, err)

	client := &http.Client{
		Transport: &oauth2.Transport{
			Source: source,
		},
	}

	resp, err := client.Get(testServer.URL + "/protected")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}

func TestNewSourceCustomDiscoveryURL(t *testing.T) {
	t.Parallel()

	key, jwkKey := generateTestKey(t)

	// customDiscoveryURL points at a path that is neither the default
	// ".well-known/openid-configuration" nor a value derivable via OIDC_DISCOVERY_PATH, proving
	// that a fully custom discoveryURL is honoured verbatim.
	const customDiscoveryPath = "/custom/metadata/endpoint"

	var defaultDiscoveryHits atomic.Int32
	var testServer *httptest.Server
	testServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			defaultDiscoveryHits.Add(1)
			http.NotFound(w, r)
		case customDiscoveryPath:
			w.Header().Set("Content-Type", "application/json")
			err := json.NewEncoder(w).Encode(map[string]string{
				"issuer":         testServer.URL,
				"token_endpoint": testServer.URL + "/oauth/token",
			})
			require.NoError(t, err)
		case "/oauth/token":
			require.NoError(t, r.ParseForm())
			parsed, err := jwt.Parse([]byte(r.FormValue("client_assertion")), jwt.WithKey(jwa.RS256(), key.Public()))
			require.NoError(t, err)
			audience, ok := parsed.Audience()
			require.True(t, ok)
			assert.Equal(t, []string{testServer.URL + "/oauth/token"}, audience)

			w.Header().Set("Content-Type", "application/json")
			err = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "generated-jwt-bearer-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
			require.NoError(t, err)
		case "/protected":
			assert.Equal(t, "Bearer generated-jwt-bearer-token", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(testServer.Close)

	source, err := NewSource(t.Context(), clientID, testServer.URL, testServer.URL+customDiscoveryPath, "", "", jwkKey)
	require.NoError(t, err)

	client := &http.Client{
		Transport: &oauth2.Transport{
			Source: source,
		},
	}

	resp, err := client.Get(testServer.URL + "/protected")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	assert.Equal(t, int32(0), defaultDiscoveryHits.Load())
}

func TestNewSourceExplicitTokenEndpoint(t *testing.T) {
	t.Parallel()

	key, jwkKey := generateTestKey(t)

	var discoveryHits atomic.Int32
	var tokenHits atomic.Int32
	var testServer *httptest.Server
	testServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			discoveryHits.Add(1)
			http.NotFound(w, r)
		case "/oauth/token":
			tokenHits.Add(1)
			require.NoError(t, r.ParseForm())
			parsed, err := jwt.Parse([]byte(r.FormValue("client_assertion")), jwt.WithKey(jwa.RS256(), key.Public()))
			require.NoError(t, err)
			audience, ok := parsed.Audience()
			require.True(t, ok)
			assert.Equal(t, []string{testServer.URL + "/oauth/token"}, audience)

			w.Header().Set("Content-Type", "application/json")
			err = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "generated-jwt-bearer-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
			require.NoError(t, err)
		case "/protected":
			assert.Equal(t, "Bearer generated-jwt-bearer-token", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(testServer.Close)

	// issuerURL is deliberately left pointing at the test server so that, if discovery were ever
	// attempted, it would hit the (404-returning) default discovery path above rather than the
	// discoveryURL used by other tests, making an accidental discovery call detectable.
	source, err := NewSource(t.Context(), clientID, testServer.URL, "", testServer.URL+"/oauth/token", "", jwkKey)
	require.NoError(t, err)

	client := &http.Client{
		Transport: &oauth2.Transport{
			Source: source,
		},
	}

	resp, err := client.Get(testServer.URL + "/protected")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	assert.Equal(t, int32(0), discoveryHits.Load())
	assert.Equal(t, int32(1), tokenHits.Load())
}

func TestDiscoveryCaching(t *testing.T) {
	t.Parallel()

	key, jwkKey := generateTestKey(t)

	// expiresIn = 0 forces the wrapping oauth2.ReuseTokenSource to request a fresh token on the
	// second call, so that a discovery hit count of 1 proves resolveTokenEndpoint's own cache
	// rather than the outer token cache.
	testServer, discoveryHits, tokenHits := newHappyPathServer(t, key, 0)

	source, err := NewSource(t.Context(), clientID, testServer.URL, "", "", "", jwkKey)
	require.NoError(t, err)

	client := &http.Client{
		Transport: &oauth2.Transport{
			Source: source,
		},
	}

	resp1, err := client.Get(testServer.URL + "/protected-a")
	require.NoError(t, err)
	defer resp1.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp1.StatusCode)

	resp2, err := client.Get(testServer.URL + "/protected-b")
	require.NoError(t, err)
	defer resp2.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp2.StatusCode)

	assert.Equal(t, int32(1), discoveryHits.Load())
	assert.Equal(t, int32(2), tokenHits.Load())
}

func TestTokenReuse(t *testing.T) {
	t.Parallel()

	key, jwkKey := generateTestKey(t)

	testServer, discoveryHits, tokenHits := newHappyPathServer(t, key, 3600)

	source, err := NewSource(t.Context(), clientID, testServer.URL, "", "", "", jwkKey)
	require.NoError(t, err)

	client := &http.Client{
		Transport: &oauth2.Transport{
			Source: source,
		},
	}

	resp1, err := client.Get(testServer.URL + "/protected-a")
	require.NoError(t, err)
	defer resp1.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp1.StatusCode)

	resp2, err := client.Get(testServer.URL + "/protected-b")
	require.NoError(t, err)
	defer resp2.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp2.StatusCode)

	assert.Equal(t, int32(1), discoveryHits.Load())
	assert.Equal(t, int32(1), tokenHits.Load())
}

func TestPrivateKeyFlowTokenEndpointError(t *testing.T) {
	t.Parallel()

	_, jwkKey := generateTestKey(t)

	testServer := newDiscoveryOnlyServer(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})

	source, err := NewSource(t.Context(), "client-id", testServer.URL, "", "", "", jwkKey)
	require.NoError(t, err)

	client := &http.Client{
		Transport: &oauth2.Transport{
			Source: source,
		},
	}

	resp, err := client.Get(testServer.URL + "/")
	if resp != nil {
		defer resp.Body.Close()
	}
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTokenExchange)
	assert.ErrorContains(t, err, "upstream token exchange failed")
	assert.ErrorContains(t, err, "boom")
}

func TestPrivateKeyFlowMalformedTokenResponse(t *testing.T) {
	t.Parallel()

	_, jwkKey := generateTestKey(t)

	testServer := newDiscoveryOnlyServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte("not-json"))
		require.NoError(t, err)
	})

	source, err := NewSource(t.Context(), "client-id", testServer.URL, "", "", "", jwkKey)
	require.NoError(t, err)

	client := &http.Client{
		Transport: &oauth2.Transport{
			Source: source,
		},
	}

	resp, err := client.Get(testServer.URL + "/")
	if resp != nil {
		defer resp.Body.Close()
	}
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTokenExchange)
	assert.ErrorContains(t, err, "failed to decode token response")
}

func TestResolveTokenEndpointDiscoveryFailures(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		handler         func(t *testing.T) http.HandlerFunc
		wantErrContains string
	}{
		"non-200 status": {
			handler: func(*testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					http.Error(w, "discovery unavailable", http.StatusInternalServerError)
				}
			},
			wantErrContains: "upstream discovery failed",
		},
		"malformed json": {
			handler: func(t *testing.T) http.HandlerFunc {
				t.Helper()
				return func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_, err := w.Write([]byte("not-json"))
					require.NoError(t, err)
				}
			},
			wantErrContains: "failed to decode discovery document",
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, jwkKey := generateTestKey(t)
			handler := tc.handler(t)

			testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/.well-known/openid-configuration" {
					http.NotFound(w, r)
					return
				}
				handler(w, r)
			}))
			t.Cleanup(testServer.Close)

			source, err := NewSource(t.Context(), "client-id", testServer.URL, "", "", "", jwkKey)
			require.NoError(t, err)

			client := &http.Client{
				Transport: &oauth2.Transport{
					Source: source,
				},
			}

			resp, err := client.Get(testServer.URL + "/")
			if resp != nil {
				defer resp.Body.Close()
			}
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrDiscovery)
			assert.ErrorContains(t, err, tc.wantErrContains)
		})
	}
}

func TestResolveTokenEndpointMissingTokenEndpoint(t *testing.T) {
	t.Parallel()

	_, jwkKey := generateTestKey(t)

	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		err := json.NewEncoder(w).Encode(map[string]string{
			"issuer": "http://" + r.Host,
		})
		require.NoError(t, err)
	}))
	t.Cleanup(testServer.Close)

	source, err := NewSource(t.Context(), "client-id", testServer.URL, "", "", "", jwkKey)
	require.NoError(t, err)

	client := &http.Client{
		Transport: &oauth2.Transport{
			Source: source,
		},
	}

	resp, err := client.Get(testServer.URL + "/")
	if resp != nil {
		defer resp.Body.Close()
	}
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDiscovery)
	assert.ErrorContains(t, err, "missing token_endpoint")
}

func TestResolveTokenEndpointIssuerTrailingSlash(t *testing.T) {
	t.Parallel()

	key, jwkKey := generateTestKey(t)

	// The discovery document reports the issuer without a trailing slash (as Keycloak does),
	// while the caller configures the issuer URL with one. The two denote the same issuer and
	// must be accepted.
	testServer, _, tokenHits := newHappyPathServer(t, key, 3600)

	source, err := NewSource(t.Context(), clientID, testServer.URL+"/", "", "", "", jwkKey)
	require.NoError(t, err)

	client := &http.Client{
		Transport: &oauth2.Transport{
			Source: source,
		},
	}

	resp, err := client.Get(testServer.URL + "/protected-a")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	assert.Equal(t, int32(1), tokenHits.Load())
}

func TestResolveTokenEndpointNoIssuerConfiguredSkipsMismatchCheck(t *testing.T) {
	t.Parallel()

	key, jwkKey := generateTestKey(t)

	// The discovery document reports an issuer that has no relationship whatsoever to the test
	// server, proving that the mismatch check is skipped entirely when the caller configures no
	// issuerURL, rather than being loosened to tolerate a near match.
	const customDiscoveryPath = "/custom/metadata/endpoint"

	var testServer *httptest.Server
	testServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case customDiscoveryPath:
			w.Header().Set("Content-Type", "application/json")
			err := json.NewEncoder(w).Encode(map[string]string{
				"issuer":         "https://unrelated-issuer.example.com",
				"token_endpoint": testServer.URL + "/oauth/token",
			})
			require.NoError(t, err)
		case "/oauth/token":
			require.NoError(t, r.ParseForm())
			parsed, err := jwt.Parse([]byte(r.FormValue("client_assertion")), jwt.WithKey(jwa.RS256(), key.Public()))
			require.NoError(t, err)
			audience, ok := parsed.Audience()
			require.True(t, ok)
			assert.Equal(t, []string{testServer.URL + "/oauth/token"}, audience)

			w.Header().Set("Content-Type", "application/json")
			err = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "generated-jwt-bearer-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
			require.NoError(t, err)
		case "/protected":
			assert.Equal(t, "Bearer generated-jwt-bearer-token", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(testServer.Close)

	// issuerURL is deliberately left empty, as it would be when only a discovery metadata URL is
	// configured and no issuer/auth endpoint is set.
	source, err := NewSource(t.Context(), clientID, "", testServer.URL+customDiscoveryPath, "", "", jwkKey)
	require.NoError(t, err)

	client := &http.Client{
		Transport: &oauth2.Transport{
			Source: source,
		},
	}

	resp, err := client.Get(testServer.URL + "/protected")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}

func TestResolveTokenEndpointIssuerMismatch(t *testing.T) {
	t.Parallel()

	_, jwkKey := generateTestKey(t)

	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		err := json.NewEncoder(w).Encode(map[string]string{
			"issuer":         "https://impostor.example.com",
			"token_endpoint": "http://" + r.Host + "/oauth/token",
		})
		require.NoError(t, err)
	}))
	t.Cleanup(testServer.Close)

	source, err := NewSource(t.Context(), "client-id", testServer.URL, "", "", "", jwkKey)
	require.NoError(t, err)

	client := &http.Client{
		Transport: &oauth2.Transport{
			Source: source,
		},
	}

	resp, err := client.Get(testServer.URL + "/")
	if resp != nil {
		defer resp.Body.Close()
	}
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDiscovery)
	assert.ErrorContains(t, err, "issuer mismatch")
}
