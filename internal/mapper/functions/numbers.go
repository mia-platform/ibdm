// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package functions

// IsNumber reports whether v is a numeric value.
// Data decoded from JSON always produces float64; int64 is accepted as a
// convenience for values set explicitly in source code.
func IsNumber(v any) bool {
	switch v.(type) {
	case float64, int64:
		return true
	default:
		return false
	}
}
