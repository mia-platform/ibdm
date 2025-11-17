// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package functions

import (
	"strings"
)

// TrimSpace removes all leading and trailing white space from the input string.
func TrimSpace(s string) string {
	return strings.TrimSpace(s)
}

// ToUpper converts the input string to upper case.
func ToUpper(s string) string {
	return strings.ToUpper(s)
}

// ToLower converts the input string to lower case.
func ToLower(s string) string {
	return strings.ToLower(s)
}
