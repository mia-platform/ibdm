// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

// Package tokenprovider defines the common Provider interface implemented by every
// client-authentication strategy used to obtain OAuth2 access tokens on behalf of ibdm
// destinations. Concrete strategies live in their own sub-packages, e.g.
// internal/tokensource/jwtclientcredential.
package tokenprovider
