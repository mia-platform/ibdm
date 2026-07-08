// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jwt"
	"golang.org/x/oauth2"
)

const (
	// clientCredentialsGrantType is the standard OAuth2 grant type used together with private-key
	// JWT client authentication, as defined by RFC 7523 section 2.2.
	clientCredentialsGrantType = "client_credentials"
	// jwtBearerClientAssertionType identifies a JWT bearer assertion used for client
	// authentication, as defined by RFC 7523 section 2.2.
	jwtBearerClientAssertionType = "urn:ietf:params:oauth:client-assertion-type:jwt-bearer" //nolint:gosec // this is an OAuth2 assertion type identifier, not a credential
	// privateKeyJWTAuthMethod identifies the client authentication method used when exchanging a
	// JWT assertion signed with a private key for an access token.
	privateKeyJWTAuthMethod = "private_key_jwt"
	// consoleClientCredentialsAudience is the fixed audience expected by the Mia-Platform Console
	// token endpoint when authenticating with a private-key JWT assertion.
	consoleClientCredentialsAudience = "console-client-credentials"
	// jwtAssertionLifetime is the validity window of the JWT assertion sent to the token endpoint.
	jwtAssertionLifetime = 5 * time.Minute
	// tokenRequestTimeout bounds how long a single token exchange request is allowed to take.
	tokenRequestTimeout = 30 * time.Second
	// maxTokenErrorBodyBytes caps how much of a non-2xx token response body is read into memory.
	maxTokenErrorBodyBytes = 1024
)

var _ oauth2.TokenSource = &privateKeyJWTTokenSource{}

// privateKeyJWTTokenSource implements oauth2.TokenSource by authenticating with a JWT assertion
// signed with a private key, following the private_key_jwt client authentication method defined
// in RFC 7523 section 2.2.
//
// The oauth2.TokenSource interface does not accept a context on Token(), so the context used for
// outgoing token requests is captured once at construction time, mirroring the behaviour of
// golang.org/x/oauth2/clientcredentials.Config.TokenSource.
type privateKeyJWTTokenSource struct {
	ctx        context.Context //nolint:containedctx // Token() has no context parameter, see doc comment above.
	clientID   string
	tokenURL   string
	privateKey jwk.Key
	httpClient *http.Client
}

// newPrivateKeyJWTTokenSource parses privateKey — either a PEM block or a raw JWK JSON document —
// and returns an oauth2.TokenSource that signs and exchanges a JWT assertion for an access token.
// Parsing errors are stored and surfaced on the first call to Token, since the constructor itself
// cannot return an error without changing the oauth2.TokenSource construction pattern.
func newPrivateKeyJWTTokenSource(ctx context.Context, clientID, tokenURL string, privateKey jwk.Key) oauth2.TokenSource {
	return &privateKeyJWTTokenSource{
		ctx:        ctx,
		clientID:   clientID,
		tokenURL:   tokenURL,
		privateKey: privateKey,
		httpClient: &http.Client{
			Timeout: tokenRequestTimeout,
		},
	}
}

// Token implements oauth2.TokenSource. It builds a signed JWT assertion and exchanges it with the
// configured token endpoint for an access token using the client_credentials grant, authenticated
// via the private_key_jwt method.
func (ts *privateKeyJWTTokenSource) Token() (*oauth2.Token, error) {
	now := time.Now()

	assertion, err := ts.signedAssertion(now)
	if err != nil {
		return nil, err
	}

	form := url.Values{}
	form.Set("grant_type", clientCredentialsGrantType)
	form.Set("client_assertion_type", jwtBearerClientAssertionType)
	form.Set("client_assertion", assertion)
	form.Set("client_id", ts.clientID)
	form.Set("token_endpoint_auth_method", privateKeyJWTAuthMethod)

	req, err := http.NewRequestWithContext(ts.ctx, http.MethodPost, ts.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := ts.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange jwt assertion: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxTokenErrorBodyBytes))
		return nil, fmt.Errorf("upstream token exchange failed: status %s: %s", resp.Status, body)
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"` //nolint:tagliatelle // OAuth2 token response uses snake_case
		TokenType   string `json:"token_type"`   //nolint:tagliatelle // OAuth2 token response uses snake_case
		ExpiresIn   int64  `json:"expires_in"`   //nolint:tagliatelle // OAuth2 token response uses snake_case
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("failed to decode token response: %w", err)
	}

	return &oauth2.Token{
		AccessToken: tokenResp.AccessToken,
		TokenType:   tokenResp.TokenType,
		Expiry:      now.Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
	}, nil
}

// signedAssertion builds and signs the JWT assertion sent to the token endpoint, using the
// signature algorithm advertised by the key, or defaulting to RS256 when the key does not
// declare one.
func (ts *privateKeyJWTTokenSource) signedAssertion(now time.Time) (string, error) {
	jti, err := uuid.NewRandom()
	if err != nil {
		return "", fmt.Errorf("failed to generate jti: %w", err)
	}

	signAlg := jwa.RS256()
	if alg, ok := ts.privateKey.Algorithm(); ok {
		if sa, ok := jwa.LookupSignatureAlgorithm(alg.String()); ok {
			signAlg = sa
		}
	}

	tok, err := jwt.NewBuilder().
		Issuer(ts.clientID).
		Subject(ts.clientID).
		Audience([]string{consoleClientCredentialsAudience}).
		JwtID(jti.String()).
		IssuedAt(now).
		Expiration(now.Add(jwtAssertionLifetime)).
		Build()
	if err != nil {
		return "", fmt.Errorf("failed to build token payload: %w", err)
	}

	signed, err := jwt.Sign(tok, jwt.WithKey(signAlg, ts.privateKey))
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}

	return string(signed), nil
}
