// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package functions

import "encoding/json"

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

// Pick creates a new map containing only the specified keys from the original map if they exist.
func Pick(object map[string]any, keys ...string) map[string]any {
	result := make(map[string]any, len(keys))
	for _, key := range keys {
		if val, exists := object[key]; exists {
			result[key] = val
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

// Set assigns the specified value to the given key in the map and returns the updated map.
func Set(key string, value any, object map[string]any) map[string]any {
	object[key] = value
	return object
}
