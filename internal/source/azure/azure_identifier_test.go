// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package azure

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	fakeazcore "github.com/Azure/azure-sdk-for-go/sdk/azcore/fake"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azeventhubs/v2"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/source"
)

const testSubscriptionID = "00000000-0000-0000-0000-000000000000"

// eventDataManagedClusterCanonicalDeleteBody deletes the same managed cluster the sync and the
// stream upsert paths import. Its subject uses a lowercase resourcegroups literal that
// arm.ParseResourceID rewrites to camelCase, so the delete path builds subjectManagedClusterID.
var eventDataManagedClusterCanonicalDeleteBody = json.RawMessage(`[
{
	"id": "00000000-0000-0000-0000-000000000000",
	"source": "/subscriptions/00000000-0000-0000-0000-000000000000",
	"specversion": "1.0",
	"type": "Microsoft.Resources.ResourceDeleteSuccess",
	"subject": "/subscriptions/00000000-0000-0000-0000-000000000000/resourcegroups/my-rg/providers/Microsoft.ContainerService/managedClusters/my-cluster",
	"time": "2020-01-01T00:00:00.0000000Z"
}]`)

// TestAllPathsEmitTheSameIdentifier drives the three ingestion paths for one managed cluster and
// checks that they agree on the values the mappings hash into the Catalog identifier. Azure feeds
// each path a different spelling of the ID, so before the normalization the sync upsert and the
// stream upsert produced two Catalog items and the stream delete targeted neither of them.
func TestAllPathsEmitTheSameIdentifier(t *testing.T) {
	t.Parallel()

	// the three spellings the fakes reproduce: Resource Graph answers with a camelCase
	// resourceGroups literal, the resource provider body lowercases it, and the delete path rebuilds
	// the ID from the event subject. Without this divergence the test would be vacuous.
	require.NotEqual(t, graphManagedClusterID, bodyManagedClusterID,
		"the sync and stream fixtures must disagree on casing")
	require.Equal(t, graphManagedClusterID, subjectManagedClusterID,
		"the delete path is expected to rebuild the Resource Graph spelling")

	paths := map[string]source.Data{
		"sync upsert":   syncedManagedCluster(t),
		"stream upsert": streamedManagedCluster(t, eventDataManagedClusterWriteBody),
		"stream delete": streamedManagedCluster(t, eventDataManagedClusterCanonicalDeleteBody),
	}

	assert.Equal(t, source.DataOperationUpsert, paths["sync upsert"].Operation)
	assert.Equal(t, source.DataOperationUpsert, paths["stream upsert"].Operation)
	assert.Equal(t, source.DataOperationDelete, paths["stream delete"].Operation)

	for pathName, data := range paths {
		assert.Equal(t, normalizedManagedClusterID, data.Values[idKey], pathName)
		assert.Equal(t, managedClustersType, data.Values[typeKey], pathName)
		assert.Equal(t, managedClustersType, data.Type, pathName)
	}
}

// syncedManagedCluster runs a sync process against the Resource Graph fake and returns the data
// emitted for the my-cluster managed cluster.
func syncedManagedCluster(t *testing.T) source.Data {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
	t.Cleanup(cancel)

	azureSource := &Source{
		config: config{
			SubscriptionID: testSubscriptionID,
			clientOptions: &arm.ClientOptions{
				ClientOptions: policy.ClientOptions{
					Transport: fakeResourceGraphTransport(t),
				},
			},
			azureCredentials: &fakeazcore.TokenCredential{},
		},
	}

	dataChannel := make(chan source.Data, 10)
	require.NoError(t, azureSource.StartSyncProcess(ctx, map[string]source.Extra{managedClustersType: nil}, dataChannel))
	close(dataChannel)

	for data := range dataChannel {
		if data.Values["name"] == "my-cluster" {
			return data
		}
	}

	require.FailNow(t, "the sync process did not emit the managed cluster")
	return source.Data{}
}

// streamedManagedCluster feeds body to the event handler and returns the single emitted data.
func streamedManagedCluster(t *testing.T, body json.RawMessage) source.Data {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
	t.Cleanup(cancel)

	client, err := armresources.NewClient(testSubscriptionID, &fakeazcore.TokenCredential{}, &arm.ClientOptions{
		ClientOptions: policy.ClientOptions{
			Transport: fakeClientTransport(t),
		},
	})
	require.NoError(t, err)

	dataChannel := make(chan source.Data, 10)
	handler := partitionEventHandler(client, map[string]source.Extra{
		managedClustersType: {apiVersionKey: managedClustersAPIVersion},
	}, dataChannel)

	handler(ctx, &azeventhubs.ReceivedEventData{EventData: azeventhubs.EventData{Body: body}})
	close(dataChannel)

	data, ok := <-dataChannel
	require.True(t, ok, "the event handler did not emit any data")
	return data
}
