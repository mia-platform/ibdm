// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package functions

import "time"

var nowFn = time.Now

// Now returns the current UTC time formatted as RFC3339.
func Now() string {
	return nowFn().UTC().Format(time.RFC3339)
}
