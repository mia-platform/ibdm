// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package gcp

import (
	"slices"
	"strings"

	"github.com/mia-platform/ibdm/internal/source"
)

type GCPEventAsset struct {
	Ancestors  []string       `json:"ancestors"`
	AssetType  string         `json:"assetType"`
	Name       string         `json:"name"`
	Resource   map[string]any `json:"resource"`
	UpdateTime string         `json:"updateTime"`
}

type GCPEventWindow struct {
	StartTime string `json:"startTime"`
}

type GCPEvent struct {
	Asset           GCPEventAsset  `json:"asset"`
	PriorAsset      GCPEventAsset  `json:"priorAsset"`
	PriorAssetState string         `json:"priorAssetState"`
	Window          GCPEventWindow `json:"window"`
	Deleted         bool           `json:"deleted"`
}

func (e GCPEvent) GetAsset() GCPEventAsset {
	return e.Asset
}

func (e GCPEvent) GetPriorAsset() GCPEventAsset {
	return e.PriorAsset
}

func (e GCPEvent) GetName() string {
	return e.Asset.Name
}

func (e GCPEvent) GetAssetType() string {
	return e.Asset.AssetType
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
