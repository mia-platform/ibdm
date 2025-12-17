// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package gcp

import (
	"slices"
	"strings"

	"github.com/mia-platform/ibdm/internal/source"
)

// GCPEvent wraps Pub/Sub payload details for Cloud Asset updates.
type GCPEvent struct {
	Asset           map[string]any `json:"asset"`
	PriorAsset      map[string]any `json:"priorAsset"`
	PriorAssetState string         `json:"priorAssetState"`
	Deleted         bool           `json:"deleted"`
}

// GetAsset returns the current asset snapshot.
func (e GCPEvent) GetAsset() map[string]any {
	return e.Asset
}

// GetPriorAsset returns the previous asset snapshot when available.
func (e GCPEvent) GetPriorAsset() map[string]any {
	return e.PriorAsset
}

// GetName returns the resource name for the asset.
func (e GCPEvent) GetName() string {
	return e.Asset["name"].(string)
}

// GetAssetType returns the asset type identifier.
func (e GCPEvent) GetAssetType() string {
	return e.Asset["assetType"].(string)
}

// Operation maps the event to the corresponding DataOperation.
func (e GCPEvent) Operation() source.DataOperation {
	if e.Deleted {
		return source.DataOperationDelete
	}

	return source.DataOperationUpsert
}

// IsTypeIn checks whether the asset type is in types, ignoring case.
func (e GCPEvent) IsTypeIn(types []string) bool {
	return slices.ContainsFunc(types, func(s string) bool {
		return strings.EqualFold(s, e.GetAssetType())
	})
}
