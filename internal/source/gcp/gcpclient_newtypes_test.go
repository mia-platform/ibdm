// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package gcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	assetpb "cloud.google.com/go/asset/apiv1/assetpb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/source"
)

const (
	dnsManagedZoneType = "dns.googleapis.com/ManagedZone"
	dnsRecordSetType   = "dns.googleapis.com/ResourceRecordSet"
	snapshotType       = "compute.googleapis.com/Snapshot"
	diskType           = "compute.googleapis.com/Disk"
	addressType        = "compute.googleapis.com/Address"
	routerType         = "compute.googleapis.com/Router"
	otherAssetType     = "storage.googleapis.com/Bucket"
)

// loadSyncAsset reads a Cloud Asset fixture from testdata/sync.
func loadSyncAsset(t *testing.T, fileName string) *assetpb.Asset {
	t.Helper()

	content, err := os.ReadFile(filepath.Join("testdata", "sync", fileName))
	require.NoError(t, err)

	var fakeAsset *assetpb.Asset
	require.NoError(t, json.Unmarshal(content, &fakeAsset))
	return fakeAsset
}

// TestStartSyncProcessNewAssetTypes checks that the asset types behind the DNS zone and record set, address, router, snapshot and disk
// item types flow through the generic sync untouched when requested, and are not emitted when another type is.
func TestStartSyncProcessNewAssetTypes(t *testing.T) {
	t.Parallel()

	allFixtures := []string{"dnszone-test.json", "recordset-test.json", "snapshot-test.json", "address-test.json", "router-test.json", "disk-test.json"}
	allTypes := []string{dnsManagedZoneType, dnsRecordSetType, snapshotType, addressType, routerType, diskType}

	testCases := map[string]struct {
		fixtures       []string
		requestedTypes []string
		expectedTypes  []string
	}{
		"dns managed zone is emitted when requested": {
			fixtures:       []string{"dnszone-test.json"},
			requestedTypes: []string{dnsManagedZoneType},
			expectedTypes:  []string{dnsManagedZoneType},
		},
		"dns record set is emitted when requested": {
			fixtures:       []string{"recordset-test.json"},
			requestedTypes: []string{dnsRecordSetType},
			expectedTypes:  []string{dnsRecordSetType},
		},
		"dns zones and record sets are told apart": {
			fixtures:       []string{"dnszone-test.json", "recordset-test.json"},
			requestedTypes: []string{dnsRecordSetType},
			expectedTypes:  []string{dnsRecordSetType},
		},
		"snapshot is emitted when requested": {
			fixtures:       []string{"snapshot-test.json"},
			requestedTypes: []string{snapshotType},
			expectedTypes:  []string{snapshotType},
		},
		"disk is emitted when requested": {
			fixtures:       []string{"disk-test.json"},
			requestedTypes: []string{diskType},
			expectedTypes:  []string{diskType},
		},
		"address is emitted when requested": {
			fixtures:       []string{"address-test.json"},
			requestedTypes: []string{addressType},
			expectedTypes:  []string{addressType},
		},
		"router is emitted when requested": {
			fixtures:       []string{"router-test.json"},
			requestedTypes: []string{routerType},
			expectedTypes:  []string{routerType},
		},
		"only the requested types are emitted": {
			fixtures:       allFixtures,
			requestedTypes: []string{addressType, routerType},
			expectedTypes:  []string{addressType, routerType},
		},
		"every new type is emitted when all are requested": {
			fixtures:       allFixtures,
			requestedTypes: allTypes,
			expectedTypes:  allTypes,
		},
		"nothing is emitted when another type is requested": {
			fixtures:       allFixtures,
			requestedTypes: []string{otherAssetType},
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			typesToSync := make(map[string]source.MappingExtras, len(testCase.requestedTypes))
			for _, requestedType := range testCase.requestedTypes {
				typesToSync[requestedType] = nil
			}

			assets := make([]*assetpb.Asset, 0, len(testCase.fixtures))
			for _, fixture := range testCase.fixtures {
				assets = append(assets, loadSyncAsset(t, fixture))
			}
			fakeAssets := filterFakeAssetsByTypes(assets, typesToSync)

			fakeClient, cleanup := newFakeAssetClient(t, fakeAssets)
			defer cleanup()

			results := make(chan source.Data, 10)
			require.NoError(t, setupGCPInstance(fakeClient).StartSyncProcess(t.Context(), typesToSync, results))
			close(results)

			emittedTypes := make([]string, 0)
			for result := range results {
				emittedTypes = append(emittedTypes, result.Type)
				assert.Equal(t, source.DataOperationUpsert, result.Operation)
				assert.False(t, result.Time.IsZero())
				assert.NotEmpty(t, result.Values["name"])
				assert.Equal(t, result.Type, result.Values["assetType"])
			}
			assert.ElementsMatch(t, testCase.expectedTypes, emittedTypes)
		})
	}
}

// streamSingleEvent publishes the Pub/Sub message in eventPath, streams it and returns what the source emits.
func streamSingleEvent(t *testing.T, typeToStream string, eventPath string) source.Data {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	config := pubSubConfig{ProjectID: "test-project", SubscriptionID: "subscription-id"}
	topicName := fmt.Sprintf(testTopicTemplate, config.ProjectID)
	subscriptionName := fmt.Sprintf("projects/%s/subscriptions/%s", config.ProjectID, config.SubscriptionID)

	message, err := os.ReadFile(eventPath)
	require.NoError(t, err)

	srv, client, cleanup := newFakePubSubClient(t, config, topicName, subscriptionName)
	defer cleanup()
	srv.Publish(topicName, message, nil)

	gcpInstance := setupInstancesForEventStreamTest(t, config, client)
	results := make(chan source.Data)
	streamDone := make(chan struct{})
	go func() {
		defer close(streamDone)
		_ = gcpInstance.StartEventStream(ctx, map[string]source.MappingExtras{typeToStream: nil}, results)
	}()

	var emitted source.Data
	select {
	case emitted = <-results:
	case <-ctx.Done():
		require.Fail(t, "timeout waiting for event")
	}

	require.NoError(t, gcpInstance.Close(ctx, time.Second))
	<-streamDone
	return emitted
}

// TestStartEventStreamNewAssetTypes checks the events of the new asset types: a modification streams the asset,
// and a deletion streams the prior asset, which is what the mappings render the identifiers of the items and of
// their relationships from, as the current asset of a deletion carries no data.
func TestStartEventStreamNewAssetTypes(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		assetType         string
		event             string
		expectedPayload   string
		expectedOperation source.DataOperation
	}{
		"router modification": {
			assetType:         routerType,
			event:             "message-gcp-router-modify.json",
			expectedPayload:   "payload-gcp-router-modify.json",
			expectedOperation: source.DataOperationUpsert,
		},
		"address deletion": {
			assetType:         addressType,
			event:             "message-gcp-address-delete.json",
			expectedPayload:   "payload-gcp-address-delete.json",
			expectedOperation: source.DataOperationDelete,
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			expectedBytes, err := os.ReadFile(filepath.Join("testdata", "event", "expected", testCase.expectedPayload))
			require.NoError(t, err)
			var expected map[string]any
			require.NoError(t, json.Unmarshal(expectedBytes, &expected))

			emitted := streamSingleEvent(t, testCase.assetType, filepath.Join("testdata", "event", "original", testCase.event))
			assert.Equal(t, testCase.assetType, emitted.Type)
			assert.Equal(t, testCase.expectedOperation, emitted.Operation)
			assert.Equal(t, expected, emitted.Values)
		})
	}
}
