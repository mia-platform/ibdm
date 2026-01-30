// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package source

import "net/http"

type Webhook struct {
	Method  string
	Path    string
	Handler func(headers http.Header, body []byte) error
}
