// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package jenkins

import (
	"context"

	"github.com/mia-platform/ibdm/internal/source"
)

const (
	pluginManagerPath = "/pluginManager"
	pluginTree        = "plugins[shortName,longName,version,active,enabled,hasUpdate,url,requiredCoreVersion,dependencies[shortName,version,optional]]"
)

// pluginList is the response of the plugin manager.
type pluginList struct {
	Plugins []pluginInfo `json:"plugins"`
}

// pluginInfo is an installed plugin.
type pluginInfo struct {
	ShortName           string             `json:"shortName"`
	LongName            string             `json:"longName"`
	Version             string             `json:"version"`
	Active              bool               `json:"active"`
	Enabled             bool               `json:"enabled"`
	HasUpdate           bool               `json:"hasUpdate"`
	URL                 string             `json:"url"`
	RequiredCoreVersion string             `json:"requiredCoreVersion"`
	Dependencies        []pluginDependency `json:"dependencies"`
}

// pluginDependency is a plugin required by another plugin.
type pluginDependency struct {
	ShortName string `json:"shortName"`
	Version   string `json:"version"`
	Optional  bool   `json:"optional"`
}

// syncPlugins emits every installed plugin. Listing the plugins requires the Overall/Administer
// or Overall/SystemRead permission.
func (s *Source) syncPlugins(ctx context.Context, results chan<- source.Data) error {
	var list pluginList
	if _, err := s.client.getJSON(ctx, pluginManagerPath+apiJSONPath, pluginTree, &list); err != nil {
		return err
	}

	for _, plugin := range list.Plugins {
		dependencies := make([]map[string]any, 0, len(plugin.Dependencies))
		for _, dependency := range plugin.Dependencies {
			dependencies = append(dependencies, map[string]any{
				"shortName": dependency.ShortName,
				"version":   dependency.Version,
				"optional":  dependency.Optional,
			})
		}

		values := map[string]any{
			"plugin": map[string]any{
				"shortName":           plugin.ShortName,
				"longName":            plugin.LongName,
				"version":             plugin.Version,
				"active":              plugin.Active,
				"enabled":             plugin.Enabled,
				"hasUpdate":           plugin.HasUpdate,
				keyURL:                plugin.URL,
				"requiredCoreVersion": plugin.RequiredCoreVersion,
				"dependencies":        dependencies,
			},
		}
		if err := send(ctx, results, pluginType, values); err != nil {
			return err
		}
	}

	return nil
}
