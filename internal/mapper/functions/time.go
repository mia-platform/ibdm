// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package functions

import "time"

var nowFn = time.Now

// Now returns the current time in UTC in RFC3339 format.
func Now() string {
	return nowFn().UTC().Format(time.RFC3339)
}
