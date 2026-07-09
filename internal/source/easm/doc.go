// SPDX-License-Identifier: AGPL-3.0-only or Commercial

// Package easm provides a source implementation that integrates EASM scan
// results into the Catalog. It reads the customer's latest completed run from
// the product backend's /data endpoint as a single paginated list of items,
// each tagged with a "type" discriminator, and emits one source.Data per item.
package easm
