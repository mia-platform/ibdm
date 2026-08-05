// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package azure

import (
	"slices"
	"strings"

	"github.com/mia-platform/ibdm/internal/logger"
)

const (
	// idKey is the payload key holding the Azure resource ID.
	idKey = "id"
	// typeKey is the payload key holding the Azure resource type.
	typeKey = "type"
	// apiVersionKey is the mapping extra key holding the api-version to use for the resource type.
	apiVersionKey = "apiVersion"
)

// normalizeResourceValues rewrites the id and type entries of a raw Azure resource payload so
// that the same resource always yields the same values, whichever Azure API produced it.
// resourceType is the configured type key and is applied verbatim.
//
// Azure does not guarantee the letter case of resource IDs: different resource providers and
// different APIs return the same ID with different casing, and the mappings hash the ID to build
// the Catalog identifier, so any casing difference creates a duplicate item. Lowercasing the
// whole ID collapses every casing variant onto one value.
//
// Lowercasing is safe only while every mapped resource type has case-insensitive names, which
// Azure documents for all the types currently mapped. Before mapping a type whose names are
// case-sensitive, such as blob containers, review docs/how-to/030_azure-source.md: folding the
// case of a case-sensitive name would make two distinct resources share one Catalog identifier.
//
// The casing Azure reported is logged when it differs from the normalized value, so that the
// resource providers returning non canonical IDs stay observable. It is deliberately not added to
// the payload: mappings must always build identifiers from id.
//
// A payload whose id is missing or not a string keeps its original value so that no item is ever
// dropped or corrupted.
func normalizeResourceValues(log logger.Logger, values map[string]any, resourceType string) {
	values[typeKey] = resourceType

	rawID, ok := values[idKey].(string)
	if !ok || rawID == "" {
		log.Warn("azure resource payload without a usable id, identifier may be unstable",
			"type", resourceType)
		return
	}

	normalizedID := strings.ToLower(rawID)
	if normalizedID != rawID {
		log.Debug("azure returned a resource id with non canonical casing",
			"type", resourceType, "id", normalizedID, "azureId", rawID)
	}

	values[idKey] = normalizedID
}

// configuredResourceType returns the configured type key matching resourceType ignoring case,
// reporting whether one was found. sortedTypes must be sorted so that the result is
// deterministic when two configured keys differ only by case.
func configuredResourceType(sortedTypes []string, resourceType string) (string, bool) {
	idx := slices.IndexFunc(sortedTypes, func(s string) bool {
		return strings.EqualFold(s, resourceType)
	})
	if idx < 0 {
		return "", false
	}

	return sortedTypes[idx], true
}
