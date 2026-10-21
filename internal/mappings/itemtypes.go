// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package mappings

const (
	// RelationshipsAPIVersion is the apiVersion of the generic Catalog relationship item type.
	RelationshipsAPIVersion = "mia-platform.eu/v1"
	// RelationshipsItemFamily is the itemFamily of the generic Catalog relationship item type.
	RelationshipsItemFamily = "relationships"
)

// ShareableItemType reports whether several mappings may write the item type of apiVersion and
// itemFamily without risking to overwrite each other's items. Only the generic relationship item
// type qualifies: a relationship identifier hashes its source, relationship type and target URNs,
// so mappings relating different kinds of items cannot produce the same identifier.
func ShareableItemType(apiVersion, itemFamily string) bool {
	return apiVersion == RelationshipsAPIVersion && itemFamily == RelationshipsItemFamily
}
