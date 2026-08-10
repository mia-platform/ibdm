// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package azure

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/mia-platform/ibdm/internal/source"
)

func TestIsFunctionApp(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		values         map[string]any
		expectedResult bool
	}{
		"the single functionapp token matches": {
			values:         map[string]any{kindKey: "functionapp"},
			expectedResult: true,
		},
		"the functionapp token in a list matches": {
			values:         map[string]any{kindKey: functionAppKindValue},
			expectedResult: true,
		},
		"the functionapp token in a longer list matches": {
			values:         map[string]any{kindKey: "functionapp,linux,container"},
			expectedResult: true,
		},
		"the token casing is ignored": {
			values:         map[string]any{kindKey: "FunctionApp,Linux"},
			expectedResult: true,
		},
		"the token spacing is ignored": {
			values:         map[string]any{kindKey: "functionapp , linux"},
			expectedResult: true,
		},
		"a web app does not match": {
			values: map[string]any{kindKey: "app"},
		},
		"a linux web app does not match": {
			values: map[string]any{kindKey: webAppKindValue},
		},
		"a token containing the term does not match": {
			values: map[string]any{kindKey: "myfunctionapp"},
		},
		"a token ending with the term does not match": {
			values: map[string]any{kindKey: "app,linux,functionapps"},
		},
		"an empty kind does not match": {
			values: map[string]any{kindKey: ""},
		},
		"a missing kind does not match": {
			values: map[string]any{idKey: normalizedWebsiteID},
		},
		"a non string kind does not match": {
			values: map[string]any{kindKey: 42},
		},
		"a nil kind does not match": {
			values: map[string]any{kindKey: nil},
		},
		"an empty payload does not match": {
			values: map[string]any{},
		},
	}

	for testName, test := range testCases {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, test.expectedResult, isFunctionApp(test.values))
		})
	}
}

func TestSubTypesFor(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		resourceType     string
		expectedTypeKeys []string
	}{
		"the configured parent type returns its sub-types": {
			resourceType:     websitesType,
			expectedTypeKeys: []string{functionAppsType},
		},
		"a lowercase parent type returns its sub-types": {
			resourceType:     "microsoft.web/sites",
			expectedTypeKeys: []string{functionAppsType},
		},
		"a shouty parent type returns its sub-types": {
			resourceType:     "MICROSOFT.WEB/SITES",
			expectedTypeKeys: []string{functionAppsType},
		},
		"a type without sub-types returns nothing": {
			resourceType: managedClustersType,
		},
		"a sub-type key is not a parent type": {
			resourceType: functionAppsType,
		},
		"an empty type returns nothing": {},
	}

	for testName, test := range testCases {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			candidates := subTypesFor(test.resourceType)
			typeKeys := make([]string, 0, len(candidates))
			for _, candidate := range candidates {
				typeKeys = append(typeKeys, candidate.typeKey)
				assert.NotNil(t, candidate.matches, "every sub-type must carry the check guarding it")
			}

			assert.ElementsMatch(t, test.expectedTypeKeys, typeKeys)
		})
	}
}

func TestIsSubTypeKey(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		typeKey        string
		expectedResult bool
	}{
		"a declared sub-type key is one": {
			typeKey:        functionAppsType,
			expectedResult: true,
		},
		"an Azure provider type is not one": {
			typeKey: websitesType,
		},
		"an unmapped type is not one": {
			typeKey: managedClustersType,
		},
		"an empty type is not one": {},
	}

	for testName, test := range testCases {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, test.expectedResult, isSubTypeKey(test.typeKey))
		})
	}
}

func TestSubTypesToEmit(t *testing.T) {
	t.Parallel()

	bothMappings := map[string]source.Extra{
		websitesType:     {apiVersionKey: websitesAPIVersion},
		functionAppsType: {apiVersionKey: websitesAPIVersion},
	}
	parentMappingOnly := map[string]source.Extra{
		websitesType: {apiVersionKey: websitesAPIVersion},
	}

	testCases := map[string]struct {
		resourceType string
		values       map[string]any
		configured   map[string]source.Extra
		isDelete     bool
		expectedKeys []string
	}{
		"an upsert of a matching payload returns the sub-type": {
			resourceType: websitesType,
			values:       map[string]any{idKey: normalizedWebsiteID, kindKey: functionAppKindValue},
			configured:   bothMappings,
			expectedKeys: []string{functionAppsType},
		},
		"an upsert of a non matching payload returns nothing": {
			resourceType: websitesType,
			values:       map[string]any{idKey: normalizedWebsiteID, kindKey: webAppKindValue},
			configured:   bothMappings,
		},
		"an upsert of a matching payload of a differently cased type returns the sub-type": {
			resourceType: "microsoft.web/sites",
			values:       map[string]any{idKey: normalizedWebsiteID, kindKey: functionAppKindValue},
			configured:   bothMappings,
			expectedKeys: []string{functionAppsType},
		},
		"a delete returns every configured sub-type whatever the payload": {
			resourceType: websitesType,
			values:       map[string]any{idKey: normalizedWebsiteID},
			configured:   bothMappings,
			isDelete:     true,
			expectedKeys: []string{functionAppsType},
		},
		"an upsert returns nothing when the sub-type mapping is not loaded": {
			resourceType: websitesType,
			values:       map[string]any{idKey: normalizedWebsiteID, kindKey: functionAppKindValue},
			configured:   parentMappingOnly,
		},
		"a delete returns nothing when the sub-type mapping is not loaded": {
			resourceType: websitesType,
			values:       map[string]any{idKey: normalizedWebsiteID},
			configured:   parentMappingOnly,
			isDelete:     true,
		},
		"a type without sub-types returns nothing": {
			resourceType: managedClustersType,
			values:       map[string]any{idKey: normalizedManagedClusterID, kindKey: "functionapp"},
			configured:   bothMappings,
		},
		"a type without sub-types returns nothing on a delete too": {
			resourceType: managedClustersType,
			values:       map[string]any{idKey: normalizedManagedClusterID},
			configured:   bothMappings,
			isDelete:     true,
		},
	}

	for testName, test := range testCases {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			assert.ElementsMatch(t, test.expectedKeys, subTypesToEmit(test.resourceType, test.values, test.configured, test.isDelete))
		})
	}
}

func TestResourceDataToEmit(t *testing.T) {
	t.Parallel()

	bothMappings := map[string]source.Extra{
		websitesType:     {apiVersionKey: websitesAPIVersion},
		functionAppsType: {apiVersionKey: websitesAPIVersion},
	}
	functionAppValues := map[string]any{
		idKey:   normalizedWebsiteID,
		typeKey: websitesType,
		kindKey: functionAppKindValue,
	}
	webAppValues := map[string]any{
		idKey:   normalizedWebsiteID,
		typeKey: websitesType,
		kindKey: webAppKindValue,
	}
	deleteValues := map[string]any{
		idKey:   normalizedWebsiteID,
		typeKey: websitesType,
	}

	testCases := map[string]struct {
		resourceType string
		values       map[string]any
		configured   map[string]source.Extra
		operation    source.DataOperation
		expectedData []source.Data
	}{
		"a matching resource emits its item and the one of its sub-type": {
			resourceType: websitesType,
			values:       functionAppValues,
			configured:   bothMappings,
			operation:    source.DataOperationUpsert,
			expectedData: []source.Data{
				{Type: websitesType, Operation: source.DataOperationUpsert, Time: testTime, Values: functionAppValues},
				{Type: functionAppsType, Operation: source.DataOperationUpsert, Time: testTime, Values: functionAppValues},
			},
		},
		"a non matching resource emits its item alone": {
			resourceType: websitesType,
			values:       webAppValues,
			configured:   bothMappings,
			operation:    source.DataOperationUpsert,
			expectedData: []source.Data{
				{Type: websitesType, Operation: source.DataOperationUpsert, Time: testTime, Values: webAppValues},
			},
		},
		"a delete broadcasts to every configured sub-type": {
			resourceType: websitesType,
			values:       deleteValues,
			configured:   bothMappings,
			operation:    source.DataOperationDelete,
			expectedData: []source.Data{
				{Type: websitesType, Operation: source.DataOperationDelete, Time: testTime, Values: deleteValues},
				{Type: functionAppsType, Operation: source.DataOperationDelete, Time: testTime, Values: deleteValues},
			},
		},
		"a type without sub-types emits its item alone": {
			resourceType: managedClustersType,
			values:       map[string]any{idKey: normalizedManagedClusterID, typeKey: managedClustersType},
			configured:   map[string]source.Extra{managedClustersType: nil},
			operation:    source.DataOperationUpsert,
			expectedData: []source.Data{
				{
					Type:      managedClustersType,
					Operation: source.DataOperationUpsert,
					Time:      testTime,
					Values:    map[string]any{idKey: normalizedManagedClusterID, typeKey: managedClustersType},
				},
			},
		},
	}

	for testName, test := range testCases {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			data := resourceDataToEmit(test.resourceType, test.values, test.configured, test.operation, testTime)

			// the item of the resource is always emitted first, so that it exists before the
			// relationship of a sub-type references it
			require.Equal(t, test.expectedData, data)
		})
	}
}

// TestResourceDataToEmitIsolatesThePayloads checks the property the emission relies on: the
// mapping functions can write into the top level of the payload they are handed, so two items must
// never share one map.
func TestResourceDataToEmitIsolatesThePayloads(t *testing.T) {
	t.Parallel()

	values := map[string]any{
		idKey:   normalizedWebsiteID,
		typeKey: websitesType,
		kindKey: functionAppKindValue,
	}

	data := resourceDataToEmit(websitesType, values, map[string]source.Extra{
		websitesType:     {apiVersionKey: websitesAPIVersion},
		functionAppsType: {apiVersionKey: websitesAPIVersion},
	}, source.DataOperationUpsert, testTime)
	require.Len(t, data, 2)

	data[0].Values["writtenByTheMapper"] = true
	assert.NotContains(t, data[1].Values, "writtenByTheMapper")

	data[1].Values["writtenByTheOtherMapper"] = true
	assert.NotContains(t, data[0].Values, "writtenByTheOtherMapper")
}

func TestWarnOrphanSubTypes(t *testing.T) {
	t.Parallel()

	const orphanMessage = "without the mapping of its parent type"

	testCases := map[string]struct {
		typesToFilter    map[string]source.Extra
		expectedMessages []string
		absentMessages   []string
	}{
		"a sub-type mapping loaded alone is reported": {
			typesToFilter:    map[string]source.Extra{functionAppsType: nil},
			expectedMessages: []string{orphanMessage, functionAppsType, websitesType},
		},
		"a sub-type mapping loaded with its parent stays silent": {
			typesToFilter:  map[string]source.Extra{websitesType: nil, functionAppsType: nil},
			absentMessages: []string{orphanMessage},
		},
		"a sub-type mapping loaded with a differently cased parent stays silent": {
			typesToFilter:  map[string]source.Extra{"microsoft.web/sites": nil, functionAppsType: nil},
			absentMessages: []string{orphanMessage},
		},
		"a parent mapping loaded alone stays silent": {
			typesToFilter:  map[string]source.Extra{websitesType: nil},
			absentMessages: []string{orphanMessage},
		},
		"an unrelated mapping stays silent": {
			typesToFilter:  map[string]source.Extra{managedClustersType: nil},
			absentMessages: []string{orphanMessage},
		},
		"no mapping stays silent": {
			absentMessages: []string{orphanMessage},
		},
	}

	for testName, test := range testCases {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			buffer := new(bytes.Buffer)
			log := logger.NewLogger(buffer)
			log.SetLevel(logger.TRACE)

			warnOrphanSubTypes(log, test.typesToFilter)

			for _, message := range test.expectedMessages {
				assert.Contains(t, buffer.String(), message)
			}
			for _, message := range test.absentMessages {
				assert.NotContains(t, buffer.String(), message)
			}
		})
	}
}
