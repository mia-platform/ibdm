// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package cmd

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/mia-platform/ibdm/internal/config"
	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/mia-platform/ibdm/internal/mappings"
)

const (
	// allMappingsSelector selects every internal mapping of the integration.
	allMappingsSelector = "all"
)

var (
	errAllWithOtherMappings = errors.New("'all' cannot be combined with other mapping names")
	errExcludeAllMappings   = errors.New("'all' is not a valid value for --" + excludeInternalMappingsFlagName)
	errEmptyMappingName     = errors.New("empty mapping name")
	errNotSyncableMapping   = errors.New("mapping is not syncable")
)

// internalSelection holds the values of the internal mapping selection flags.
type internalSelection struct {
	include    []string
	exclude    []string
	includeSet bool
	excludeSet bool
}

// selectedMappings is the outcome of an internal selection.
type selectedMappings struct {
	// configs are the internal mappings to use, in lexical order of name.
	configs []*config.MappingConfig
	// notSyncable are the internal mappings skipped under sync because they are not syncable.
	notSyncable []string
}

// normalizeSelectors trims the values of flag, drops duplicates and rejects empty elements.
// A flag given with an empty value is reported and treated as omitted.
func normalizeSelectors(log logger.Logger, flag string, values []string, set bool) ([]string, bool, error) {
	if !set {
		return nil, false, nil
	}
	if len(values) == 0 || (len(values) == 1 && strings.TrimSpace(values[0]) == "") {
		log.Warn("--" + flag + " was given an empty value and selects nothing: check the variable or template that produced it")
		return nil, false, nil
	}

	selectors := make([]string, 0, len(values))
	for _, value := range values {
		selector := strings.TrimSpace(value)
		if selector == "" {
			return nil, false, fmt.Errorf("%w in --%s %q: remove the extra comma", errEmptyMappingName, flag, strings.Join(values, ","))
		}
		if !slices.Contains(selectors, selector) {
			selectors = append(selectors, selector)
		}
	}
	return selectors, true, nil
}

// unknownMappingError reports a selector naming no internal mapping of source. When the name
// matches an external mapping, the error says the selection flags do not apply to it.
func unknownMappingError(flag, source, name string, internalNames, externalNames []string) error {
	hint := ""
	if slices.Contains(externalNames, name) {
		hint = fmt.Sprintf("; %q is a --%s mapping, and those are always loaded in full: the selection flags apply to internal mappings only", name, mappingFileFlagName)
	}
	return fmt.Errorf("%w %q in --%s: the internal mappings of %s are %s%s", mappings.ErrUnknownMapping, name, flag, source, strings.Join(internalNames, ", "), hint)
}

// selectInternalMappings applies the selection to internal, the internal mappings of source. The
// result is the included mappings minus the excluded ones. Under sync, a non-syncable mapping
// named explicitly in the include list is an error, while one reached through 'all' is skipped.
func selectInternalMappings(log logger.Logger, source string, selection internalSelection, internal []*config.MappingConfig, externalNames []string, syncOnly bool) (selectedMappings, error) {
	include, includeSet, err := normalizeSelectors(log, includeInternalMappingsFlagName, selection.include, selection.includeSet)
	if err != nil {
		return selectedMappings{}, err
	}
	exclude, excludeSet, err := normalizeSelectors(log, excludeInternalMappingsFlagName, selection.exclude, selection.excludeSet)
	if err != nil {
		return selectedMappings{}, err
	}

	internalNames := make([]string, 0, len(internal))
	byName := make(map[string]*config.MappingConfig, len(internal))
	for _, mapping := range internal {
		internalNames = append(internalNames, mapping.Name)
		byName[mapping.Name] = mapping
	}
	slices.Sort(internalNames)

	selected, explicit, err := includedNames(source, include, internalNames, externalNames)
	if err != nil {
		return selectedMappings{}, err
	}

	if err := checkExcludedNames(log, source, exclude, excludeSet, includeSet, selected, internalNames, externalNames); err != nil {
		return selectedMappings{}, err
	}

	result := selectedMappings{configs: make([]*config.MappingConfig, 0, len(selected))}
	for _, name := range selected {
		if slices.Contains(exclude, name) {
			continue
		}

		mapping := byName[name]
		if syncOnly && !mapping.IsSyncable() {
			if explicit {
				return selectedMappings{}, fmt.Errorf("%w: internal mapping %q cannot be produced by sync: remove it from --%s", errNotSyncableMapping, name, includeInternalMappingsFlagName)
			}
			result.notSyncable = append(result.notSyncable, name)
			continue
		}
		result.configs = append(result.configs, mapping)
	}
	return result, nil
}

// checkExcludedNames validates the exclude list against internalNames and warns when it has no
// effect: without an include list, or for a name the include list did not select.
func checkExcludedNames(log logger.Logger, source string, exclude []string, excludeSet, includeSet bool, selected, internalNames, externalNames []string) error {
	if slices.Contains(exclude, allMappingsSelector) {
		return errExcludeAllMappings
	}
	for _, name := range exclude {
		if !slices.Contains(internalNames, name) {
			return unknownMappingError(excludeInternalMappingsFlagName, source, name, internalNames, externalNames)
		}
	}

	if excludeSet && !includeSet {
		log.Warn("--" + excludeInternalMappingsFlagName + " without --" + includeInternalMappingsFlagName + " selects no internal mapping")
		return nil
	}
	for _, name := range exclude {
		if !slices.Contains(selected, name) {
			log.Warn("excluded internal mapping was not selected", "mapping", name)
		}
	}
	return nil
}

// includedNames resolves the include list against internalNames, in lexical order. explicit
// reports whether the names were listed one by one rather than reached through 'all'.
func includedNames(source string, include, internalNames, externalNames []string) ([]string, bool, error) {
	if slices.Contains(include, allMappingsSelector) {
		if len(include) > 1 {
			return nil, false, fmt.Errorf("%w in --%s %q", errAllWithOtherMappings, includeInternalMappingsFlagName, strings.Join(include, ","))
		}
		return internalNames, false, nil
	}

	for _, name := range include {
		if !slices.Contains(internalNames, name) {
			return nil, false, unknownMappingError(includeInternalMappingsFlagName, source, name, internalNames, externalNames)
		}
	}
	selected := slices.Clone(include)
	slices.Sort(selected)
	return selected, true, nil
}
