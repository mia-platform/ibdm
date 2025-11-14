// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package functions

import "encoding/json"

func ToJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}

	return string(data)
}
