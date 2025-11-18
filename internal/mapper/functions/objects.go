// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package functions

import "encoding/json"

// ToJSON converts a value to its JSON string representation.
func ToJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}

	return string(data)
}

// Pluck extracts the values associated with the specified key from a slice of maps and returns them
// as a slice.
func Pluck(key string, objects []map[string]any) []any {
	result := make([]any, 0)
	for _, obj := range objects {
		if val, exists := obj[key]; exists {
			result = append(result, val)
		}
	}

	return result
}

// Get retrieves the value associated with the specified key from the map. If the key does not exist,
// it returns the provided default value.
func Get(key string, object map[string]any, defaultValue any) any {
	if val, exists := object[key]; exists {
		return val
	}

	return defaultValue
}
