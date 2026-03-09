// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package functions

import "encoding/json"

// Object builds a map from alternating key and value arguments.
func Object(keyAndValues ...any) map[string]any {
	obj := make(map[string]any)
	parametersLength := len(keyAndValues)
	for idx := 0; idx < parametersLength; idx += 2 {
		key := castToString(keyAndValues[idx])
		var value any = nil
		if idx+1 < parametersLength {
			value = keyAndValues[idx+1]
		}

		obj[key] = value
	}

	return obj
}

// ToJSON converts a value to its JSON string representation.
func ToJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}

	return string(data)
}

// Pick creates a map containing only the specified keys found in object.
func Pick(object map[string]any, keys ...string) map[string]any {
	result := make(map[string]any, len(keys))
	for _, key := range keys {
		if val, exists := object[key]; exists {
			result[key] = val
		}
	}

	return result
}

// Get returns object[key] or defaultValue when the key is missing.
func Get(key string, object map[string]any, defaultValue any) any {
	if val, exists := object[key]; exists {
		return val
	}

	return defaultValue
}

// Set stores value at key in object and returns object.
func Set(key string, value any, object map[string]any) map[string]any {
	object[key] = value
	return object
}
