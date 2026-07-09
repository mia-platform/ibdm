// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

// Package clientcredentialsource implements tokensource.Provider using the private_key_jwt
// client authentication method defined in RFC 7523 section 2.2: a JWT assertion is signed with
// a private key and exchanged with a token endpoint for an access token via the
// client_credentials grant.
package clientcredentialsource
