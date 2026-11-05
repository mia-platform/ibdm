// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package jenkins

import (
	"maps"
	"slices"
)

// libraryRecord is a shared library loaded by a build.
type libraryRecord struct {
	Name      string   `json:"name"`
	Version   string   `json:"version"`
	Trusted   bool     `json:"trusted"`
	Changelog bool     `json:"changelog"`
	Variables []string `json:"variables"`
}

// libraryAggregate gathers every load of a shared library across the last completed builds.
type libraryAggregate struct {
	versions  map[string]struct{}
	variables map[string]struct{}
	usages    []map[string]any
}

// libraryCollector groups the shared libraries loaded by the last completed builds by name.
type libraryCollector struct {
	byName map[string]*libraryAggregate
}

// newLibraryCollector returns an empty libraryCollector.
func newLibraryCollector() *libraryCollector {
	return &libraryCollector{byName: make(map[string]*libraryAggregate)}
}

// add records the shared libraries loaded by build, the last completed build of workflow.
func (c *libraryCollector) add(workflow jobItem, kind string, multibranch map[string]any, build *buildInfo) {
	for _, record := range buildLibraries(build) {
		aggregate, ok := c.byName[record.Name]
		if !ok {
			aggregate = &libraryAggregate{
				versions:  make(map[string]struct{}),
				variables: make(map[string]struct{}),
			}
			c.byName[record.Name] = aggregate
		}

		aggregate.versions[record.Version] = struct{}{}
		for _, variable := range record.Variables {
			aggregate.variables[variable] = struct{}{}
		}

		usage := map[string]any{
			keyKind:       kind,
			keyFullName:   workflow.FullName,
			keyURL:        workflow.URL,
			"version":     record.Version,
			"trusted":     record.Trusted,
			"changelog":   record.Changelog,
			"buildNumber": build.Number,
		}
		if multibranch != nil {
			usage["multibranch"] = multibranch
		}
		aggregate.usages = append(aggregate.usages, usage)
	}
}

// values returns the values of every collected shared library, ordered by name.
func (c *libraryCollector) values() []map[string]any {
	names := slices.Sorted(maps.Keys(c.byName))
	out := make([]map[string]any, 0, len(names))
	for _, name := range names {
		aggregate := c.byName[name]
		out = append(out, map[string]any{
			"library": map[string]any{
				keyName:     name,
				"versions":  slices.Sorted(maps.Keys(aggregate.versions)),
				"variables": slices.Sorted(maps.Keys(aggregate.variables)),
			},
			"usages": aggregate.usages,
		})
	}
	return out
}

// buildLibraries returns the shared libraries loaded by build, or nil when build is nil.
func buildLibraries(build *buildInfo) []libraryRecord {
	if build == nil {
		return nil
	}

	var records []libraryRecord
	for _, action := range build.Actions {
		records = append(records, action.Libraries...)
	}
	return records
}

// workflowLibraries returns the shared libraries loaded by build as exposed on a workflow.
func workflowLibraries(build *buildInfo) []map[string]any {
	records := buildLibraries(build)
	out := make([]map[string]any, 0, len(records))
	for _, record := range records {
		out = append(out, map[string]any{
			keyName:   record.Name,
			"version": record.Version,
			"trusted": record.Trusted,
		})
	}
	return out
}
