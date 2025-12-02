// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package gcp

import (
	"slices"
	"strings"

	"github.com/mia-platform/ibdm/internal/source"
)

type GCPEvent struct {
	Asset           map[string]any `json:"asset"`
	PriorAsset      map[string]any `json:"priorAsset"`
	PriorAssetState string         `json:"priorAssetState"`
	Deleted         bool           `json:"deleted"`
}

func (e GCPEvent) GetAsset() map[string]any {
	return e.Asset
}

func (e GCPEvent) GetPriorAsset() map[string]any {
	return e.PriorAsset
}

func (e GCPEvent) GetName() string {
	return e.Asset["name"].(string)
}

func (e GCPEvent) GetAssetType() string {
	return e.Asset["assetType"].(string)
}

func (e GCPEvent) Operation() source.DataOperation {
	if e.Deleted {
		return source.DataOperationDelete
	}

	return source.DataOperationUpsert
}

func (e GCPEvent) IsTypeIn(types []string) bool {
	return slices.ContainsFunc(types, func(s string) bool {
		return strings.EqualFold(s, e.GetAssetType())
	})
}
