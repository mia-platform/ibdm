// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package catalog

import (
	"context"
	"net/http"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

// NewTransport creates an HTTP transport configured with either a static token, private-key JWT
// client authentication, or client-credentials flow.
func NewTransport(ctx context.Context, token, tokenURL, clientID, clientSecret string, keys *Keys) http.RoundTripper {
	var source oauth2.TokenSource
	switch {
	case len(token) > 0:
		source = oauth2.StaticTokenSource(&oauth2.Token{
			AccessToken: token,
			TokenType:   "Bearer",
		})
	case len(clientID) > 0 && len(clientSecret) > 0:
		config := clientcredentials.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			TokenURL:     tokenURL,
			AuthStyle:    oauth2.AuthStyleInHeader,
		}

		source = config.TokenSource(ctx)
	case len(clientID) > 0 && keys != nil && keys.PrivateKey != nil:
		source = newPrivateKeyJWTTokenSource(ctx, clientID, tokenURL, keys.PrivateKey)
	}

	if source == nil {
		return http.DefaultTransport
	}

	return &oauth2.Transport{
		Source: source,
	}
}
