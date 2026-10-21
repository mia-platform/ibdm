// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package mappings

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestShareableItemType(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		apiVersion string
		itemFamily string
		expected   bool
	}{
		"the generic relationship item type":       {apiVersion: "mia-platform.eu/v1", itemFamily: "relationships", expected: true},
		"relationships of another version":         {apiVersion: "mia-platform.eu/v2", itemFamily: "relationships", expected: false},
		"relationships of a subdomain":             {apiVersion: "k8s.mia-platform.eu/v1", itemFamily: "relationships", expected: false},
		"another family of the relationship group": {apiVersion: "mia-platform.eu/v1", itemFamily: "relationshiptypes", expected: false},
		"a user item type":                         {apiVersion: "my-org.example.com/v1", itemFamily: "relationships", expected: false},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, test.expected, ShareableItemType(test.apiVersion, test.itemFamily))
		})
	}
}
