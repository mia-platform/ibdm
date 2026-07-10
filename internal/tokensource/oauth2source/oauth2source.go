// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package oauth2source

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/google/uuid"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jwt"
	"golang.org/x/oauth2"

	"github.com/mia-platform/ibdm/internal/tokensource"
)

const (
	// clientCredentialsGrantType is the standard OAuth2 grant type used together with private-key
	// JWT client authentication, as defined by RFC 7523 section 2.2.
	clientCredentialsGrantType = "client_credentials"
	// jwtBearerClientAssertionType identifies a JWT bearer assertion used for client
	// authentication, as defined by RFC 7523 section 2.2.
	jwtBearerClientAssertionType = "urn:ietf:params:oauth:client-assertion-type:jwt-bearer" //nolint:gosec
	// privateKeyJWTAuthMethod identifies the client authentication method used when exchanging a
	// JWT assertion signed with a private key for an access token.
	privateKeyJWTAuthMethod = "private_key_jwt"
	// jwtAssertionLifetime is the validity window of the JWT assertion sent to the token endpoint.
	jwtAssertionLifetime = 5 * time.Minute
	// tokenRequestTimeout bounds how long a single token exchange request or discovery request is
	// allowed to take.
	tokenRequestTimeout = 30 * time.Second
	// maxTokenErrorBodyBytes caps how much of a non-2xx token response body is read into memory.
	maxTokenErrorBodyBytes = 1024
)

var (
	// ErrTokenExchange wraps failures encountered while exchanging a JWT assertion for an access token.
	ErrTokenExchange = errors.New("oauth2source token exchange")

	// ErrDiscovery wraps failures encountered while resolving the token endpoint via OIDC discovery.
	ErrDiscovery = errors.New("oauth2source discovery")

	// ErrConfig wraps failures encountered while loading oauth2source configuration from the environment.
	ErrConfig = errors.New("oauth2source config")
)

// config holds the environment-driven settings for oauth2source.
type config struct {
	// DiscoveryPath is the well-known path suffix used to discover OIDC provider metadata, as
	// defined by the OpenID Connect Discovery specification. It is configurable to accommodate
	// issuers that serve their discovery document at a non-standard path.
	DiscoveryPath string `env:"OIDC_DISCOVERY_PATH" envDefault:".well-known/openid-configuration"`
}

// loadConfigFromEnv parses the oauth2source configuration from environment variables.
func loadConfigFromEnv() (*config, error) {
	cfg := new(config)
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrConfig, err)
	}
	return cfg, nil
}

var _ tokensource.Source = &source{}

// source implements tokensource.Source by authenticating with a JWT assertion signed with
// a private key, following the private_key_jwt client authentication method defined in RFC 7523
// section 2.2. The token endpoint and the JWT audience are resolved lazily via OIDC discovery
// against issuerURL.
//
// The oauth2.TokenSource interface does not accept a context on Token(), so the context used for
// outgoing token and discovery requests is captured once at construction time, mirroring the
// behaviour of golang.org/x/oauth2/clientcredentials.Config.TokenSource.
type source struct {
	ctx           context.Context //nolint:containedctx // Token() has no context parameter, see doc comment above.
	clientID      string
	issuerURL     string
	privateKey    jwk.Key
	discoveryPath string
	httpClient    *http.Client

	mu            sync.Mutex
	tokenEndpoint string
}

// NewSource returns a tokensource.Source that signs a JWT assertion with privateKey and
// exchanges it, via the private_key_jwt method defined in RFC 7523 section 2.2, for an access
// token authenticating as clientID. The token endpoint and the JWT audience are resolved via
// OIDC discovery against issuerURL. The returned source automatically reuses tokens until near
// expiry, see oauth2.ReuseTokenSource. Configuration is validated at construction time.
func NewSource(ctx context.Context, clientID, issuerURL string, privateKey jwk.Key) (tokensource.Source, error) {
	cfg, err := loadConfigFromEnv()
	if err != nil {
		return nil, err
	}

	inner := &source{
		ctx:           ctx,
		clientID:      clientID,
		issuerURL:     issuerURL,
		privateKey:    privateKey,
		discoveryPath: cfg.DiscoveryPath,
		httpClient: &http.Client{
			Timeout: tokenRequestTimeout,
		},
	}

	return oauth2.ReuseTokenSource(nil, inner), nil
}

// Token implements oauth2.TokenSource by signing a JWT assertion with the private key and
// exchanging it with the token endpoint for an access token. The token endpoint is resolved via
// OIDC discovery against issuerURL on first use and cached for subsequent calls.
func (p *source) Token() (*oauth2.Token, error) {
	now := time.Now()

	tokenEndpoint, err := p.resolveTokenEndpoint(p.ctx)
	if err != nil {
		return nil, err
	}

	assertion, err := p.signedAssertion(now, tokenEndpoint)
	if err != nil {
		return nil, err
	}

	form := url.Values{}
	form.Set("grant_type", clientCredentialsGrantType)
	form.Set("client_assertion_type", jwtBearerClientAssertionType)
	form.Set("client_assertion", assertion)
	form.Set("client_id", p.clientID)
	form.Set("token_endpoint_auth_method", privateKeyJWTAuthMethod)

	req, err := http.NewRequestWithContext(p.ctx, http.MethodPost, tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("%w: failed to build token request: %w", ErrTokenExchange, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to exchange jwt assertion: %w", ErrTokenExchange, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxTokenErrorBodyBytes))
		return nil, fmt.Errorf("%w: upstream token exchange failed: status %s: %s", ErrTokenExchange, resp.Status, body)
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"` //nolint:tagliatelle // OAuth2 token response uses snake_case
		TokenType   string `json:"token_type"`   //nolint:tagliatelle // OAuth2 token response uses snake_case
		ExpiresIn   int64  `json:"expires_in"`   //nolint:tagliatelle // OAuth2 token response uses snake_case
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("%w: failed to decode token response: %w", ErrTokenExchange, err)
	}

	return &oauth2.Token{
		AccessToken: tokenResp.AccessToken,
		TokenType:   tokenResp.TokenType,
		Expiry:      now.Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
	}, nil
}

// signedAssertion builds and signs the JWT assertion sent to tokenEndpoint, using the
// signature algorithm advertised by the key, or defaulting to RS256 when the key does not
// declare one. tokenEndpoint is used as the assertion's audience, as required by RFC 7523
// section 3.
func (p *source) signedAssertion(now time.Time, tokenEndpoint string) (string, error) {
	jti, err := uuid.NewRandom()
	if err != nil {
		return "", fmt.Errorf("%w: failed to generate jti: %w", ErrTokenExchange, err)
	}

	signAlg := jwa.RS256()
	if alg, ok := p.privateKey.Algorithm(); ok {
		if sa, ok := jwa.LookupSignatureAlgorithm(alg.String()); ok {
			signAlg = sa
		}
	}

	tok, err := jwt.NewBuilder().
		Issuer(p.clientID).
		Subject(p.clientID).
		Audience([]string{tokenEndpoint}).
		JwtID(jti.String()).
		IssuedAt(now).
		Expiration(now.Add(jwtAssertionLifetime)).
		Build()
	if err != nil {
		return "", fmt.Errorf("%w: failed to build token payload: %w", ErrTokenExchange, err)
	}

	signed, err := jwt.Sign(tok, jwt.WithKey(signAlg, p.privateKey))
	if err != nil {
		return "", fmt.Errorf("%w: failed to sign token: %w", ErrTokenExchange, err)
	}

	return string(signed), nil
}

// discoveryDocument is the minimal subset of OIDC provider metadata, as defined by the OpenID
// Connect Discovery specification, that this package relies on.
type discoveryDocument struct {
	Issuer        string `json:"issuer"`
	TokenEndpoint string `json:"token_endpoint"` //nolint:tagliatelle // OIDC discovery document uses snake_case
}

// resolveTokenEndpoint returns the token endpoint advertised by the issuer's OIDC discovery
// document, fetching and validating it on first use and caching the result for subsequent calls.
// A failed discovery is never cached, so a later call retries it from scratch.
func (p *source) resolveTokenEndpoint(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.tokenEndpoint != "" {
		return p.tokenEndpoint, nil
	}

	discoveryURL, err := url.JoinPath(p.issuerURL, p.discoveryPath)
	if err != nil {
		return "", fmt.Errorf("%w: failed to build discovery url: %w", ErrDiscovery, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return "", fmt.Errorf("%w: failed to build discovery request: %w", ErrDiscovery, err)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: failed to fetch discovery document: %w", ErrDiscovery, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxTokenErrorBodyBytes))
		return "", fmt.Errorf("%w: upstream discovery failed: status %s: %s", ErrDiscovery, resp.Status, body)
	}

	var doc discoveryDocument
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return "", fmt.Errorf("%w: failed to decode discovery document: %w", ErrDiscovery, err)
	}

	if normalizeIssuer(doc.Issuer) != normalizeIssuer(p.issuerURL) {
		return "", fmt.Errorf("%w: issuer mismatch: expected %q, got %q", ErrDiscovery, p.issuerURL, doc.Issuer)
	}

	if doc.TokenEndpoint == "" {
		return "", fmt.Errorf("%w: discovery document is missing token_endpoint", ErrDiscovery)
	}

	p.tokenEndpoint = doc.TokenEndpoint

	return p.tokenEndpoint, nil
}

// normalizeIssuer canonicalizes an issuer identifier for the OIDC issuer check, tolerating the
// differences that commonly arise between a configured issuer URL and the value advertised in a
// discovery document without weakening the check: a trailing slash and differing case in the
// scheme or host, none of which are significant per RFC 3986. The path is left case-sensitive,
// as OIDC issuer paths (such as Keycloak realm names) are. If the value does not parse as a URL
// it is compared verbatim, minus any trailing slash.
func normalizeIssuer(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return strings.TrimRight(raw, "/")
	}

	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Path = strings.TrimRight(parsed.Path, "/")

	return parsed.String()
}
