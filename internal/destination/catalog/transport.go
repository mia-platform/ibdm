// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package catalog

import (
	"context"
	"net/http"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"

	"github.com/mia-platform/ibdm/internal/jwk"
	"github.com/mia-platform/ibdm/internal/tokensource/oauth2source"
)

// NewTransport creates an HTTP transport configured with either a static token, private-key JWT
// client authentication, or client-credentials flow. authEndpointMetadata and catalogTokenEndpoint
// are only used by the private-key JWT branch: see oauth2source.NewSource for their meaning.
func NewTransport(ctx context.Context, token, tokenURL, clientID, clientSecret, authEndpointMetadata, catalogTokenEndpoint string, keys *jwk.Keys) (http.RoundTripper, error) {
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
		oauth2Source, err := oauth2source.NewSource(ctx, clientID, tokenURL, authEndpointMetadata, catalogTokenEndpoint, keys.PrivateKey)
		if err != nil {
			return nil, err
		}
		source = oauth2Source
	}

	if source == nil {
		return http.DefaultTransport, nil
	}

	return &oauth2.Transport{
		Source: source,
	}, nil
}
