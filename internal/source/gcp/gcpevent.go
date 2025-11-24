// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package gcp

import "github.com/mia-platform/ibdm/internal/source"

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

func (e GCPEvent) Name() string {
	return e.Asset.Name
}

func (e GCPEvent) AssetType() string {
	return e.Asset.AssetType
}

func (e GCPEvent) Resource() map[string]any {
	return e.Asset.Resource
}

func (e GCPEvent) Operation() source.DataOperation {
	switch {
	case e.Deleted:
		return source.DataOperationDelete
	case e.PriorAssetState == "DOES_NOT_EXIST":
		return source.DataOperationUpsert
	case e.PriorAssetState == "PRESENT":
		return source.DataOperationUpsert
	default:
		return source.DataOperationUpsert
	}
}
