// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package azure

import (
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/mia-platform/ibdm/internal/source"
)

const (
	// websitesType is the Azure provider type of the App Service sites.
	websitesType = "Microsoft.Web/sites"

	// functionAppsType is the type key of the Function App sub-type mapping. It is an internal
	// dispatch key and not an Azure provider type: it is never sent to Azure and it is never used
	// to build a Resource Graph query.
	functionAppsType = "functionapps"

	// kindKey is the payload key holding the kind of an Azure resource.
	kindKey = "kind"

	// functionAppKind is the kind token marking an App Service site as a Function App.
	functionAppKind = "functionapp"

	// kindSeparator separates the tokens of a kind payload value.
	kindSeparator = ","
)

// subType couples the type key of a sub-type mapping with the predicate deciding whether a parent
// resource payload must also produce it. Keeping the two together makes it impossible to declare a
// sub-type without the check guarding it.
type subType struct {
	// typeKey is the type declared by the mapping file of the sub-type.
	typeKey string

	// matches reports whether values, the retrieved payload of the parent resource, must also
	// produce this sub-type. It is never consulted on a delete, where the payload carries only the
	// resource id.
	matches func(values map[string]any) bool
}

// subTypes maps an Azure provider type to the sub-types it can additionally produce. A sub-type is
// a specialised item, described by its own mapping file and related to the item of the resource it
// was derived from, so declaring one requires an entry here and can never be done by configuration
// alone.
//
// The order of a list is the order its sub-types are emitted in.
var subTypes = map[string][]subType{
	websitesType: {
		{typeKey: functionAppsType, matches: isFunctionApp},
	},
}

var (
	// subTypesByParent indexes subTypes by lowercased parent type so that a lookup ignores the
	// letter case the mapping file used for the Azure provider type, consistently with
	// configuredResourceType.
	subTypesByParent = indexSubTypesByParent()

	// subTypeKeys holds every type key declared in subTypes. Deriving it from the dictionary keeps
	// the exclusion of the sub-type keys from the Azure queries in step with their emission.
	subTypeKeys = collectSubTypeKeys()
)

// indexSubTypesByParent builds the case insensitive index of subTypes.
func indexSubTypesByParent() map[string][]subType {
	index := make(map[string][]subType, len(subTypes))
	for parentType, candidates := range subTypes {
		index[strings.ToLower(parentType)] = candidates
	}

	return index
}

// collectSubTypeKeys builds the set of every type key declared in subTypes.
func collectSubTypeKeys() map[string]struct{} {
	keys := make(map[string]struct{}, len(subTypes))
	for _, candidates := range subTypes {
		for _, candidate := range candidates {
			keys[candidate.typeKey] = struct{}{}
		}
	}

	return keys
}

// isSubTypeKey reports whether typeKey is the type key of a sub-type mapping. Such a key is an
// internal dispatch key: it must never be used to build a Resource Graph query, nor be matched
// against the resource type carried by the subject of an event.
func isSubTypeKey(typeKey string) bool {
	_, found := subTypeKeys[typeKey]
	return found
}

// subTypesFor returns the sub-types resourceType can additionally produce, ignoring its letter case.
func subTypesFor(resourceType string) []subType {
	return subTypesByParent[strings.ToLower(resourceType)]
}

// isFunctionApp reports whether the kind of a resource carries the functionapp token.
//
// Azure spells kind as a comma separated list of tokens, such as "app", "app,linux" or
// "functionapp,linux,container", so the tokens are compared one by one and a kind such as
// "myfunctionapp" does not qualify. A missing, empty or non string kind reports false, so that a
// payload without the discriminant produces the item of the resource alone instead of failing.
func isFunctionApp(values map[string]any) bool {
	kind, ok := values[kindKey].(string)
	if !ok {
		return false
	}

	for token := range strings.SplitSeq(kind, kindSeparator) {
		if strings.EqualFold(strings.TrimSpace(token), functionAppKind) {
			return true
		}
	}

	return false
}

// subTypesToEmit returns the type keys of the sub-types a resource of resourceType must
// additionally produce, in the order the dictionary declares them.
//
// On an upsert every predicate is evaluated against values, the payload retrieved from Azure. On a
// delete the payload carries only the resource id, so no predicate can run and every configured
// sub-type is returned: deleting a sub-type item that was never created is inert, because the
// Catalog publish reports no per item outcome.
//
// A sub-type whose mapping file is not loaded is never returned, so a deployment configuring no
// sub-type mapping behaves exactly as one running against an empty dictionary.
func subTypesToEmit(resourceType string, values map[string]any, configured map[string]source.Extra, isDelete bool) []string {
	candidates := subTypesFor(resourceType)
	if len(candidates) == 0 {
		return nil
	}

	keys := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if _, found := configured[candidate.typeKey]; !found {
			continue
		}

		if !isDelete && !candidate.matches(values) {
			continue
		}

		keys = append(keys, candidate.typeKey)
	}

	return keys
}

// resourceDataToEmit builds every source.Data a retrieved resource must produce: the item of the
// resource itself first, so that it exists before the sub-types relate to it, then one item for
// every sub-type resourceType additionally produces.
//
// Every predicate runs and every payload is built before the caller sends the first value, because
// the pipeline starts reading a value as soon as it is sent: cloning after a send would race the
// mapper.
//
// The payload of a sub-type is a shallow copy of the one of its parent, because the mapping
// functions can write into the top level of the map they are handed and two items must never share
// it. The nested values stay shared, which is why the mapping templates must treat their input
// payload as read only.
func resourceDataToEmit(resourceType string, values map[string]any, configured map[string]source.Extra, operation source.DataOperation, timestamp time.Time) []source.Data {
	emitted := subTypesToEmit(resourceType, values, configured, operation == source.DataOperationDelete)

	data := make([]source.Data, 0, 1+len(emitted))
	data = append(data, source.Data{
		Type:      resourceType,
		Operation: operation,
		Time:      timestamp,
		Values:    values,
	})

	for _, typeKey := range emitted {
		data = append(data, source.Data{
			Type:      typeKey,
			Operation: operation,
			Time:      timestamp,
			Values:    maps.Clone(values),
		})
	}

	return data
}

// warnOrphanSubTypes logs a warning for every configured sub-type mapping whose parent type mapping
// is not configured. A sub-type is emitted only while its parent resource is handled, so such a
// mapping can never produce any item. It is a warning, and not an error, because the source always
// skips the configuration it cannot use.
func warnOrphanSubTypes(log logger.Logger, typesToFilter map[string]source.Extra) {
	configuredTypes := slices.Sorted(maps.Keys(typesToFilter))
	for _, parentType := range slices.Sorted(maps.Keys(subTypes)) {
		if _, configured := configuredResourceType(configuredTypes, parentType); configured {
			continue
		}

		for _, candidate := range subTypes[parentType] {
			if _, found := typesToFilter[candidate.typeKey]; !found {
				continue
			}

			log.Warn("sub-type mapping configured without the mapping of its parent type, it will never produce any item",
				"type", candidate.typeKey, "parentType", parentType)
		}
	}
}
