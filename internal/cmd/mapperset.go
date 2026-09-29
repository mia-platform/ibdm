// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package cmd

import (
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/mia-platform/ibdm/internal/config"
	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/mia-platform/ibdm/internal/mapper"
	"github.com/mia-platform/ibdm/internal/mappings"
	"github.com/mia-platform/ibdm/internal/pipeline"
)

// minSharedWriters is the number of mappings from which an item type is shared.
const minSharedWriters = 2

var (
	errReservedMappingName = errors.New("reserved mapping name")
	errReservedDomain      = errors.New("reserved domain")
	errSharedItemTypes     = errors.New("several mappings write the same item type")
)

// mapperRequest describes the mappings a run asks for.
type mapperRequest struct {
	source               string
	selection            internalSelection
	externalPaths        []string
	syncOnly             bool
	allowSharedItemTypes bool
	// internalMappings returns the internal mappings of a source; nil means mappings.ForSource.
	internalMappings func(string) ([]*config.MappingConfig, error)
}

// resolveMappers builds the mappers of a run: the selected internal mappings first, then every
// external mapping, checked as one set and compiled. It returns no mapper when nothing is selected.
func resolveMappers(log logger.Logger, request mapperRequest) (map[string][]pipeline.DataMapper, error) {
	internalMappings := request.internalMappings
	if internalMappings == nil {
		internalMappings = mappings.ForSource
	}

	internal, err := internalMappings(request.source)
	if err != nil {
		return nil, err
	}

	external, err := loadMappingConfigs(request.externalPaths)
	if err != nil {
		return nil, err
	}
	if err := refuseReservedDomains(request.source, external, mappingNames(internal)); err != nil {
		return nil, err
	}
	externalNames := mappingNames(external)

	selected, err := selectInternalMappings(log, request.source, request.selection, internal, externalNames, request.syncOnly)
	if err != nil {
		return nil, err
	}

	if err := checkExternalNames(external, append(mappingNames(selected.configs), selected.notSyncable...)); err != nil {
		return nil, err
	}

	externalKept := make([]*config.MappingConfig, 0, len(external))
	externalNotSyncable := make([]string, 0)
	for _, mapping := range external {
		if request.syncOnly && !mapping.IsSyncable() {
			externalNotSyncable = append(externalNotSyncable, mapping.Name)
			continue
		}
		externalKept = append(externalKept, mapping)
	}

	final := slices.Concat(selected.configs, externalKept)
	if err := checkSharedItemTypes(log, final, request.allowSharedItemTypes); err != nil {
		return nil, err
	}
	warnDifferentExtras(log, selected.configs, externalKept)
	reportResolvedSet(log, request.source, selected, externalKept, externalNotSyncable)

	if len(final) == 0 {
		return nil, nil
	}
	return buildMappers(final)
}

// refuseReservedDomains rejects an external mapping whose root apiVersion is under a reserved
// system domain: only internal mappings may write system item types. The error points to the
// internal mapping of the same name, when there is one.
func refuseReservedDomains(source string, external []*config.MappingConfig, internalNames []string) error {
	for _, mapping := range external {
		domain, reserved := mappings.ReservedAPIVersionDomain(mapping.APIVersion)
		if !reserved {
			continue
		}

		fix := "publish it to your own domain instead"
		if slices.Contains(internalNames, mapping.Name) {
			fix = fmt.Sprintf("use --%s=%s to load the internal mapping instead", includeInternalMappingsFlagName, mapping.Name)
		}
		return fmt.Errorf("%w %s in %q: its apiVersion %q is written by the internal mappings of %s only, %s",
			errReservedDomain, domain, mapping.Path(), mapping.APIVersion, source, fix)
	}
	return nil
}

// mappingNames returns the names of configs, in their order.
func mappingNames(configs []*config.MappingConfig) []string {
	names := make([]string, 0, len(configs))
	for _, mapping := range configs {
		names = append(names, mapping.Name)
	}
	return names
}

// checkExternalNames rejects an external mapping named 'all', or named like a selected internal
// mapping, with the fixes the user can apply.
func checkExternalNames(external []*config.MappingConfig, internalNames []string) error {
	for _, mapping := range external {
		if mapping.Name == allMappingsSelector {
			return fmt.Errorf("%w %q in %q: it selects every internal mapping, set a different name in the file", errReservedMappingName, mapping.Name, mapping.Path())
		}
		if slices.Contains(internalNames, mapping.Name) {
			return fmt.Errorf("%w %q: %q is also the name of a selected internal mapping: set a different name in the file, "+
				"rename it, or leave the internal mapping out of the selection", config.ErrDuplicateMappingName, mapping.Name, mapping.Path())
		}
	}
	return nil
}

// checkSharedItemTypes rejects mappings writing the same item type, whose items may overwrite
// each other, unless allowed: then every shared item type is reported as a warning.
func checkSharedItemTypes(log logger.Logger, configs []*config.MappingConfig, allowed bool) error {
	writers := make(map[string][]string)
	for _, mapping := range configs {
		itemType := mapping.APIVersion + " " + mapping.ItemFamily
		writers[itemType] = append(writers[itemType], mapping.Name)
	}

	shared := make([]string, 0)
	for _, itemType := range slices.Sorted(maps.Keys(writers)) {
		if len(writers[itemType]) < minSharedWriters {
			continue
		}
		if allowed {
			log.Warn("mappings write the same item type, their items may overwrite each other", "itemType", itemType, "mappings", writers[itemType])
			continue
		}
		shared = append(shared, fmt.Sprintf("%s (%s)", itemType, strings.Join(writers[itemType], ", ")))
	}

	if len(shared) > 0 {
		return fmt.Errorf("%w, and their items may overwrite each other when their identifiers overlap: %s: pass --%s to start anyway",
			errSharedItemTypes, strings.Join(shared, "; "), allowSharedItemTypesFlagName)
	}
	return nil
}

// warnDifferentExtras warns when an external mapping shares the type of an internal mapping but
// declares a different root extra: the source then fetches the data a second way.
func warnDifferentExtras(log logger.Logger, internal, external []*config.MappingConfig) {
	for _, externalMapping := range external {
		if len(externalMapping.Extra) == 0 {
			continue
		}
		for _, internalMapping := range internal {
			if internalMapping.Type != externalMapping.Type || maps.EqualFunc(internalMapping.Extra, externalMapping.Extra, reflect.DeepEqual) {
				continue
			}
			log.Warn("external mapping declares a root extra different from the internal mapping of its type, the source fetches its data separately",
				"type", externalMapping.Type, "externalMapping", externalMapping.Name, "internalMapping", internalMapping.Name)
		}
	}
}

// reportResolvedSet logs, before any work begins, the mappings the run uses and the ones skipped.
func reportResolvedSet(log logger.Logger, source string, selected selectedMappings, external []*config.MappingConfig, externalNotSyncable []string) {
	skipped := []any{}
	if notSyncable := slices.Concat(selected.notSyncable, externalNotSyncable); len(notSyncable) > 0 {
		skipped = []any{"skippedNotSyncable", notSyncable}
	}

	if len(selected.configs) == 0 && len(external) == 0 {
		log.Info(source+": no mappings selected, nothing to do", skipped...)
		return
	}

	details := append([]any{"internalMappings", mappingNames(selected.configs), "externalMappings", mappingNames(external)}, skipped...)
	log.Info(fmt.Sprintf("%s: using %d internal mappings and %d external mappings", source, len(selected.configs), len(external)), details...)
}

// buildMappers compiles configs into typed mappers. Every mapping sharing a type is kept, in order.
func buildMappers(configs []*config.MappingConfig) (map[string][]pipeline.DataMapper, error) {
	typedMappers := make(map[string][]pipeline.DataMapper)
	for _, mapping := range configs {
		templates := mapping.Mappings
		compiled, err := mapper.New(templates.Identifier, templates.Metadata, templates.Spec, templates.Extra, mapper.WithCreateIf(mapping.CreateIf))
		if err != nil {
			return nil, err
		}

		typedMappers[mapping.Type] = append(typedMappers[mapping.Type], pipeline.DataMapper{
			Name:       mapping.Name,
			APIVersion: mapping.APIVersion,
			ItemFamily: mapping.ItemFamily,
			Mapper:     compiled,
			Extra:      mapping.Extra,
		})
	}
	return typedMappers, nil
}
