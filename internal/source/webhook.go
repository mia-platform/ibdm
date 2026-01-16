// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package source

type Webhook struct {
	Method  string
	Path    string
	Handler func() error
}
