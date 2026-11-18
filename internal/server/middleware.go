// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package server

import (
	"net/http"
	"strings"
)

// middleware wraps an http.Handler with additional behaviour.
type middleware func(http.Handler) http.Handler

// chain wraps h with mws so that mws[0] is the outermost middleware, the first to see a request.
func chain(h http.Handler, mws ...middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

// routingPath returns the form of path used to match routes: lowercase and without trailing
// slashes, so that routes match regardless of case and of a trailing slash.
func routingPath(path string) string {
	path = strings.ToLower(path)
	if len(path) > 1 {
		path = strings.TrimRight(path, "/")
		if path == "" {
			path = "/"
		}
	}
	return path
}

// normalizePath routes every request on routingPath of its path. The request seen by the inner
// handlers carries the normalized path; middleware placed before it sees the original one.
func normalizePath(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := routingPath(r.URL.Path)
		if path == r.URL.Path {
			next.ServeHTTP(w, r)
			return
		}

		normalized := new(http.Request)
		*normalized = *r
		normalizedURL := *r.URL
		normalizedURL.Path = path
		normalizedURL.RawPath = ""
		normalized.URL = &normalizedURL
		next.ServeHTTP(w, normalized)
	})
}

// limitBody rejects request bodies larger than maxBytes: reading past the limit fails with an
// *http.MaxBytesError.
func limitBody(maxBytes int64) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}
