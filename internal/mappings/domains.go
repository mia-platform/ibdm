// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package mappings

import "strings"

// reservedDomains are the system domains a user-supplied mapping may not publish to. The list is
// fixed on purpose: an overridable list would be an escape hatch under another name.
var reservedDomains = []string{
	"mia-platform.eu",
	"mia-care.io",
	"mia-fintech.io",
}

// ReservedDomain reports the reserved domain group belongs to: group is reserved when it equals a
// reserved domain or is one of its subdomains. A near miss such as notmia-platform.eu.example.com
// is not reserved, and neither is mia-platform-experimental.eu.
func ReservedDomain(group string) (string, bool) {
	for _, domain := range reservedDomains {
		if group == domain || strings.HasSuffix(group, "."+domain) {
			return domain, true
		}
	}
	return "", false
}

// ReservedAPIVersionDomain reports the reserved domain of a mapping root apiVersion, whose group is
// the part before the first "/": both console.mia-platform.eu/v1 and mia-platform.eu/v2 are reserved.
func ReservedAPIVersionDomain(apiVersion string) (string, bool) {
	group, _, _ := strings.Cut(apiVersion, "/")
	return ReservedDomain(group)
}
