// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

// Package oauth2source implements tokensource.Source using the private_key_jwt
// client authentication method defined in RFC 7523 section 2.2: a JWT assertion is signed with
// a private key and exchanged for an access token via the client_credentials grant.
//
// The token endpoint and the JWT audience are not configured directly; instead, they are
// resolved via OIDC discovery (the ".well-known/openid-configuration" document, overridable via
// the OIDC_DISCOVERY_PATH environment variable) against a configured issuer URL, following an
// OAuth2/OIDC provider setup such as Keycloak. The resolved token endpoint is cached for the
// lifetime of the source, and discovery is retried on the next call whenever a previous attempt
// failed.
//
// The oauth2.TokenSource returned by NewSource automatically reuses tokens until they are near
// expiry, via oauth2.ReuseTokenSource.
package oauth2source
