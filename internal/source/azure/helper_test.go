// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package azure

// Fixtures of the two App Service sites the sub-type tests import, shared by the sync, the stream
// and the dictionary tests because the sub-type emission runs on the retrieved payload whichever
// path retrieved it: my-function carries the functionapp kind token and my-site does not.
const (
	// websitesAPIVersion is the api-version the website mappings declare.
	websitesAPIVersion = "2025-03-01"

	// functionAppKindValue and webAppKindValue are two kind values Azure returns for an App Service
	// site: only the first one carries the functionapp token.
	functionAppKindValue = "functionapp,linux"
	webAppKindValue      = "app,linux"

	// the ids as Azure spells them, with a camelCase resourceGroups literal and a canonically cased
	// provider and type, and as the source normalizes them.
	azureFunctionAppID      = "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/my-rg/providers/Microsoft.Web/sites/my-function"
	normalizedFunctionAppID = "/subscriptions/00000000-0000-0000-0000-000000000000/resourcegroups/my-rg/providers/microsoft.web/sites/my-function"
	azureWebsiteID          = "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/my-rg/providers/Microsoft.Web/sites/my-site"
	normalizedWebsiteID     = "/subscriptions/00000000-0000-0000-0000-000000000000/resourcegroups/my-rg/providers/microsoft.web/sites/my-site"
)
