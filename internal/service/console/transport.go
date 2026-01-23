// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package console

import (
	"context"
	"net/http"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
	"golang.org/x/oauth2/jwt"
)

// newTransport creates an HTTP transport configured with either a static token or a client-credentials flow.
func newTransport(ctx context.Context, tokenURL, clientID, clientSecret string) http.RoundTripper {
	var source oauth2.TokenSource
	if len(clientID) > 0 && len(clientSecret) > 0 {
		config := clientcredentials.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			TokenURL:     tokenURL,
			AuthStyle:    oauth2.AuthStyleInHeader,
		}

		source = config.TokenSource(ctx)
	}

	if source == nil {
		return http.DefaultTransport
	}

	return &oauth2.Transport{
		Source: source,
	}
}

func newTransportWithJWT(ctx context.Context, tokenURL, privateKey, privateKeyID, clientID string) http.RoundTripper {
	config := &jwt.Config{
		Subject:      clientID,
		PrivateKey:   []byte(privateKey),
		PrivateKeyID: privateKeyID,
		TokenURL:     tokenURL,
	}

	return &oauth2.Transport{
		Source: config.TokenSource(ctx),
	}
}
