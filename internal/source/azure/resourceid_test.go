// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package azure

import (
	"bytes"
	"maps"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/logger"
)

const (
	managedClustersType = "Microsoft.ContainerService/managedClusters"

	// The same managed cluster as Azure spells it on the three ingestion paths: Resource Graph and
	// the event subject use camelCase resourceGroups, while the resource provider response body
	// lowercases it.
	graphManagedClusterID   = "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/my-rg/providers/Microsoft.ContainerService/managedClusters/my-cluster"
	bodyManagedClusterID    = "/subscriptions/00000000-0000-0000-0000-000000000000/resourcegroups/my-rg/providers/Microsoft.ContainerService/managedClusters/my-cluster"
	subjectManagedClusterID = "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/my-rg/providers/Microsoft.ContainerService/managedClusters/my-cluster"

	// normalizedManagedClusterID is what every path must converge to.
	normalizedManagedClusterID = "/subscriptions/00000000-0000-0000-0000-000000000000/resourcegroups/my-rg/providers/microsoft.containerservice/managedclusters/my-cluster"
)

func TestNormalizeResourceValues(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		values         map[string]any
		resourceType   string
		expectedValues map[string]any
	}{
		"mixed case id is lowered and type is forced to the configured key": {
			values: map[string]any{
				"id":   graphManagedClusterID,
				"type": "microsoft.containerservice/managedclusters",
				"name": "my-cluster",
			},
			resourceType: managedClustersType,
			expectedValues: map[string]any{
				"id":   normalizedManagedClusterID,
				"type": managedClustersType,
				"name": "my-cluster",
			},
		},
		"already lowercase id is left unchanged": {
			values: map[string]any{
				"id": normalizedManagedClusterID,
			},
			resourceType: managedClustersType,
			expectedValues: map[string]any{
				"id":   normalizedManagedClusterID,
				"type": managedClustersType,
			},
		},
		"shouty id is fully lowered": {
			values: map[string]any{
				"id": "/SUBSCRIPTIONS/00000000-0000-0000-0000-000000000000/RESOURCEGROUPS/MY-RG/PROVIDERS/MICROSOFT.WEB/SITES/MY-SITE",
			},
			resourceType: "Microsoft.Web/sites",
			expectedValues: map[string]any{
				"id":   "/subscriptions/00000000-0000-0000-0000-000000000000/resourcegroups/my-rg/providers/microsoft.web/sites/my-site",
				"type": "Microsoft.Web/sites",
			},
		},
		"malformed id is lowered without being parsed": {
			values: map[string]any{
				"id": "Not-An-Azure-Id",
			},
			resourceType: managedClustersType,
			expectedValues: map[string]any{
				"id":   "not-an-azure-id",
				"type": managedClustersType,
			},
		},
		"missing id leaves the other values untouched": {
			values: map[string]any{
				"name":     "my-rg",
				"location": "westeurope",
			},
			resourceType: "Microsoft.Resources/resourceGroups",
			expectedValues: map[string]any{
				"name":     "my-rg",
				"location": "westeurope",
				"type":     "Microsoft.Resources/resourceGroups",
			},
		},
		"non string id is preserved": {
			values: map[string]any{
				"id": 42,
			},
			resourceType: managedClustersType,
			expectedValues: map[string]any{
				"id":   42,
				"type": managedClustersType,
			},
		},
		"nil id is preserved": {
			values: map[string]any{
				"id": nil,
			},
			resourceType: managedClustersType,
			expectedValues: map[string]any{
				"id":   nil,
				"type": managedClustersType,
			},
		},
		"empty id is preserved": {
			values: map[string]any{
				"id": "",
			},
			resourceType: managedClustersType,
			expectedValues: map[string]any{
				"id":   "",
				"type": managedClustersType,
			},
		},
		"unrelated keys are never touched": {
			values: map[string]any{
				"id":   graphManagedClusterID,
				"name": "my-cluster",
				"properties": map[string]any{
					"provisioningState": "Succeeded",
					"fqdn":              "my-cluster.example.com",
				},
				"tags": map[string]any{"Env": "Prod"},
			},
			resourceType: managedClustersType,
			expectedValues: map[string]any{
				"id":   normalizedManagedClusterID,
				"name": "my-cluster",
				"properties": map[string]any{
					"provisioningState": "Succeeded",
					"fqdn":              "my-cluster.example.com",
				},
				"tags": map[string]any{"Env": "Prod"},
				"type": managedClustersType,
			},
		},
	}

	for testName, test := range testCases {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			values := maps.Clone(test.values)
			normalizeResourceValues(nullTestLogger(t), values, test.resourceType)

			// comparing the whole map also guards against the helper adding any field beyond id
			// and type: a payload visible raw id would silently reinstate the duplicate item bug.
			require.Equal(t, test.expectedValues, values)

			rawID, ok := test.values[idKey].(string)
			if !ok || rawID == "" {
				return
			}

			normalizedID, ok := values[idKey].(string)
			require.True(t, ok)
			require.True(t, strings.EqualFold(rawID, normalizedID), "normalization must only change letter case")
		})
	}
}

func TestNormalizeResourceValuesConvergesDivergentCasings(t *testing.T) {
	t.Parallel()

	graphValues := map[string]any{idKey: graphManagedClusterID}
	bodyValues := map[string]any{idKey: bodyManagedClusterID}
	subjectValues := map[string]any{idKey: subjectManagedClusterID}

	require.NotEqual(t, graphValues[idKey], bodyValues[idKey], "the fixtures must differ before normalization")

	for _, values := range []map[string]any{graphValues, bodyValues, subjectValues} {
		normalizeResourceValues(nullTestLogger(t), values, managedClustersType)
	}

	assert.Equal(t, normalizedManagedClusterID, graphValues[idKey])
	assert.Equal(t, normalizedManagedClusterID, bodyValues[idKey])
	assert.Equal(t, normalizedManagedClusterID, subjectValues[idKey])
}

func TestNormalizeResourceValuesIsIdempotent(t *testing.T) {
	t.Parallel()

	values := map[string]any{
		idKey:  graphManagedClusterID,
		"name": "my-cluster",
	}

	normalizeResourceValues(nullTestLogger(t), values, managedClustersType)
	once := maps.Clone(values)

	normalizeResourceValues(nullTestLogger(t), values, managedClustersType)
	assert.Equal(t, once, values)
}

func TestNormalizeResourceValuesLogging(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		values           map[string]any
		expectedMessages []string
		absentMessages   []string
	}{
		"divergent casing is reported": {
			values:           map[string]any{idKey: bodyManagedClusterID},
			expectedMessages: []string{"non canonical casing", bodyManagedClusterID},
		},
		"canonical casing stays silent": {
			values:         map[string]any{idKey: normalizedManagedClusterID},
			absentMessages: []string{"non canonical casing"},
		},
		"unusable id is reported": {
			values:           map[string]any{"name": "my-cluster"},
			expectedMessages: []string{"without a usable id"},
			absentMessages:   []string{"non canonical casing"},
		},
	}

	for testName, test := range testCases {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			buffer := new(bytes.Buffer)
			log := logger.NewLogger(buffer)
			log.SetLevel(logger.TRACE)

			normalizeResourceValues(log, maps.Clone(test.values), managedClustersType)

			for _, message := range test.expectedMessages {
				assert.Contains(t, buffer.String(), message)
			}
			for _, message := range test.absentMessages {
				assert.NotContains(t, buffer.String(), message)
			}
		})
	}
}

func TestConfiguredResourceType(t *testing.T) {
	t.Parallel()

	configuredTypes := []string{"Microsoft.ContainerService/managedClusters", "Microsoft.Web/sites"}

	testCases := map[string]struct {
		sortedTypes   []string
		resourceType  string
		expectedType  string
		expectedFound bool
	}{
		"exact match": {
			sortedTypes:   configuredTypes,
			resourceType:  managedClustersType,
			expectedType:  managedClustersType,
			expectedFound: true,
		},
		"lowercase match returns the configured key": {
			sortedTypes:   configuredTypes,
			resourceType:  "microsoft.containerservice/managedclusters",
			expectedType:  managedClustersType,
			expectedFound: true,
		},
		"shouty match returns the configured key": {
			sortedTypes:   configuredTypes,
			resourceType:  "MICROSOFT.WEB/SITES",
			expectedType:  "Microsoft.Web/sites",
			expectedFound: true,
		},
		"unconfigured type is not found": {
			sortedTypes:  configuredTypes,
			resourceType: "Microsoft.Compute/virtualMachines",
		},
		"nil slice is not found": {
			resourceType: managedClustersType,
		},
		"empty slice is not found": {
			sortedTypes:  []string{},
			resourceType: managedClustersType,
		},
		"keys differing only by case resolve deterministically": {
			sortedTypes:   []string{"Microsoft.Web/sites", "microsoft.web/sites"},
			resourceType:  "MICROSOFT.WEB/SITES",
			expectedType:  "Microsoft.Web/sites",
			expectedFound: true,
		},
	}

	for testName, test := range testCases {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			resourceType, found := configuredResourceType(test.sortedTypes, test.resourceType)
			assert.Equal(t, test.expectedFound, found)
			assert.Equal(t, test.expectedType, resourceType)
		})
	}
}

// nullTestLogger returns a logger discarding every entry.
func nullTestLogger(tb testing.TB) logger.Logger {
	tb.Helper()
	return logger.FromContext(tb.Context())
}
