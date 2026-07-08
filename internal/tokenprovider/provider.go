// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package tokenprovider

import "golang.org/x/oauth2"

// Provider is implemented by every client-authentication strategy able to produce an
// oauth2.TokenSource for authenticating outgoing requests. Each strategy lives in its own
// sub-package under internal/tokenprovider and exposes a constructor returning a Provider, e.g.
// jwtclientcredential.NewProvider.
type Provider interface {
	oauth2.TokenSource
}
