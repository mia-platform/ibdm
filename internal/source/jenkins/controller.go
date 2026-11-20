// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package jenkins

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

const (
	// controllerValuesKey is the key of the controller reference in the values of every data type.
	controllerValuesKey = "controller"
	keyControllerKey    = "key"
)

// controllerRef identifies the controller the source reads from. Item names are unique only
// within one controller, so the mappings combine them with key to tell the items of different
// controllers apart.
type controllerRef struct {
	// key is the configured controller URL without scheme, default port and trailing slash, with
	// a lowercase host.
	key string
	// url is the configured controller URL without trailing slash.
	url string
}

// newControllerRef returns the reference of the controller reached at rawURL, the validated
// JENKINS_URL. The reference is derived from the configured URL only: the URLs found in the
// responses follow the root URL setting of the controller, which can be empty or differ.
func newControllerRef(rawURL string) (controllerRef, error) {
	key, err := controllerKey(rawURL)
	if err != nil {
		return controllerRef{}, err
	}

	return controllerRef{key: key, url: strings.TrimRight(rawURL, "/")}, nil
}

// controllerKey normalises rawURL into the key of the controller: the scheme is dropped, so that
// moving from http to https keeps the identity, the host is lowercased, the default port of the
// scheme is dropped, any other port is kept, and the path is kept as is without trailing slash.
func controllerKey(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("%w: %s is not a valid URL", ErrInvalidEnvVariable, urlEnvName)
	}

	host := strings.ToLower(parsed.Hostname())
	port := parsed.Port()
	if (parsed.Scheme == "http" && port == "80") || (parsed.Scheme == "https" && port == "443") {
		port = ""
	}

	switch {
	case port != "":
		host = net.JoinHostPort(host, port)
	case strings.Contains(host, ":"):
		// An IPv6 address keeps its brackets, so that it cannot be read as a host and a port.
		host = "[" + host + "]"
	}

	return strings.TrimRight(host+parsed.EscapedPath(), "/"), nil
}

// values returns the controller reference as exposed to the mappings.
func (c controllerRef) values() map[string]any {
	return map[string]any{
		keyControllerKey: c.key,
		keyURL:           c.url,
	}
}
