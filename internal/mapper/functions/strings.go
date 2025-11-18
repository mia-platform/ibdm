// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package functions

import (
	"encoding/base64"
	"strings"
)

// TrimSpace removes all leading and trailing white space from the input string.
func TrimSpace(s string) string {
	return strings.TrimSpace(s)
}

// TrimPrefix removes the provided prefix from the input string.
func TrimPrefix(prefix, s string) string {
	return strings.TrimPrefix(s, prefix)
}

// TrimSuffix removes the provided suffix from the input string.
func TrimSuffix(suffix, s string) string {
	return strings.TrimSuffix(s, suffix)
}

// ToUpper converts the input string to upper case.
func ToUpper(s string) string {
	return strings.ToUpper(s)
}

// ToLower converts the input string to lower case.
func ToLower(s string) string {
	return strings.ToLower(s)
}

// Truncate truncates the input string to the specified length. If length is
// negative, it truncates from the end of the string. If length is positive,
// it truncates from the beginning of the string. If length is outside the
// bounds of the string, it returns the original string.
func Truncate(length int, s string) string {
	// length is negative, truncate from the end
	if length < 0 && len(s)+length > 0 {
		return s[len(s)+length:]
	}

	// length is positive, truncate from the beginning
	if length >= 0 && len(s) > length {
		return s[:length]
	}

	// if length is outside the bounds of the string, we return the original string
	return s
}

// Split splits the input string by the provided separator and returns a slice of substrings.
func Split(sep, s string) []string {
	return strings.Split(s, sep)
}

// EncodeBase64 encodes the input string to a Base64 encoded string.
func EncodeBase64(input string) string {
	return base64.StdEncoding.EncodeToString([]byte(input))
}

// DecodeBase64 decodes a Base64 encoded string back to its original form.
func DecodeBase64(input string) (string, error) {
	decoded, err := base64.StdEncoding.DecodeString(input)
	if err != nil {
		return "", err
	}

	return string(decoded), nil
}
