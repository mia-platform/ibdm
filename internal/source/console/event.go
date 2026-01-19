// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package console

type event struct {
	Type string         `json:"type"`
	Data map[string]any `json:"data"`
}

func (e event) GetName() string {
	return e.Data["name"].(string)
}

func (e event) GetType() string {
	return e.Type
}
