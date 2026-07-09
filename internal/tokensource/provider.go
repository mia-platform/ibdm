// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package tokenprovider

import "golang.org/x/oauth2"

// Source is implemented by every client-authentication strategy able to produce an
// oauth2.TokenSource for authenticating outgoing requests. Each strategy lives in its own
// sub-package under internal/tokensource and exposes a constructor returning a Source, e.g.
// jwtclientcredential.NewProvider.
type Source interface {
	oauth2.TokenSource
}
