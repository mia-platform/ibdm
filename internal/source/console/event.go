// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package console

import (
	"slices"
	"strings"
	"time"

	"github.com/mia-platform/ibdm/internal/source"
)

type event struct {
	EventName      string         `json:"eventName"`
	EventTimestamp int64          `json:"eventTimestamp"`
	Payload        map[string]any `json:"payload"`
}

func (e event) GetName() string {
	return e.Payload["name"].(string)
}

func (e event) UnixEventTimestamp() time.Time {
	return time.Unix(e.EventTimestamp, 0)
}

func (e event) Resource() string {
	parts := strings.Split(e.EventName, "_")
	if len(parts) > 0 {
		resourceSlice := parts[0 : len(parts)-1]
		if len(resourceSlice) > 0 {
			return strings.Join(resourceSlice, "_")
		}
		return resourceSlice[0]
	}
	return ""
}

func (e event) Operation() source.DataOperation {
	if strings.HasSuffix(strings.ToLower(e.EventName), "deleted") ||
		strings.HasSuffix(strings.ToLower(e.EventName), "removed") {
		return source.DataOperationDelete
	}
	return source.DataOperationUpsert
}

func (e event) IsTypeIn(types []string) bool {
	resource := e.Resource()
	return slices.ContainsFunc(types, func(s string) bool {
		return strings.EqualFold(s, resource)
	})
}
