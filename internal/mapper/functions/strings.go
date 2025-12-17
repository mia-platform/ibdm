// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package functions

import (
	"encoding/base64"
	"fmt"
	"strings"
)

// Quote wraps the string representation of the input value in double quotes.
func Quote(s any) string {
	return fmt.Sprintf("%q", castToString(s))
}

// TrimSpace removes all leading and trailing whitespace from the input string.
func TrimSpace(s string) string {
	return strings.TrimSpace(s)
}

// TrimPrefix removes prefix from s when present.
func TrimPrefix(prefix, s string) string {
	return strings.TrimPrefix(s, prefix)
}

// TrimSuffix removes suffix from s when present.
func TrimSuffix(suffix, s string) string {
	return strings.TrimSuffix(s, suffix)
}

// Replace substitutes every occurrence of toChange with toBe in s.
func Replace(toChange, toBe, s string) string {
	return strings.ReplaceAll(s, toChange, toBe)
}

// ToUpper converts the input string to uppercase.
func ToUpper(s string) string {
	return strings.ToUpper(s)
}

// ToLower converts the input string to lowercase.
func ToLower(s string) string {
	return strings.ToLower(s)
}

// Truncate keeps a portion of s based on length.
// A positive length returns the prefix, a negative length preserves the suffix,
// and values outside the string bounds leave s unchanged.
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

// Split splits the input string by the given separator and returns the resulting substrings.
func Split(sep, s string) []string {
	return strings.Split(s, sep)
}

// EncodeBase64 encodes the input string to its Base64 representation.
func EncodeBase64(input string) string {
	return base64.StdEncoding.EncodeToString([]byte(input))
}

// DecodeBase64 decodes a Base64-encoded string and returns the original value.
func DecodeBase64(input string) (string, error) {
	decoded, err := base64.StdEncoding.DecodeString(input)
	if err != nil {
		return "", err
	}

	return string(decoded), nil
}

// castToString converts obj to its string representation.
func castToString(obj any) string {
	switch v := obj.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}
