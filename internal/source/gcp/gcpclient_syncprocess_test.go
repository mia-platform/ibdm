// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package gcp

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"testing"
	"time"

	asset "cloud.google.com/go/asset/apiv1"
	assetpb "cloud.google.com/go/asset/apiv1/assetpb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/mia-platform/ibdm/internal/source"
)

type fakeAssetServiceServer struct {
	assetpb.UnimplementedAssetServiceServer

	assets []*assetpb.Asset
}

func filterFakeAssetsByTypes(assets []*assetpb.Asset, types []string) []*assetpb.Asset {
	typeSet := make(map[string]struct{})
	for _, t := range types {
		typeSet[t] = struct{}{}
	}

	filtered := make([]*assetpb.Asset, 0)
	for _, a := range assets {
		if _, ok := typeSet[a.GetAssetType()]; ok {
			filtered = append(filtered, a)
		}
	}
	return filtered
}

func newFakeAssetClient(t *testing.T, fakeAssets []*assetpb.Asset) (*asset.Client, func()) {
	t.Helper()

	fakeSrv := &fakeAssetServiceServer{
		assets: fakeAssets,
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	gsrv := grpc.NewServer()
	assetpb.RegisterAssetServiceServer(gsrv, fakeSrv)
	fakeServerAddr := l.Addr().String()
	go func() {
		if err := gsrv.Serve(l); err != nil {
			gsrv.Stop()
			t.Logf("server listener failed: %v", err)
			panic(err)
		}
	}()

	time.Sleep(10 * time.Millisecond)

	client, err := asset.NewClient(t.Context(),
		option.WithEndpoint(fakeServerAddr),
		option.WithoutAuthentication(),
		option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
	)

	if err != nil {
		gsrv.Stop()
		t.Fatalf("failed to create fake asset client: %v", err)
	}

	return client, func() {
		_ = client.Close()
		gsrv.Stop()
		_ = l.Close()
	}
}

func setupGCPInstance(fakeClient *asset.Client) *GCPSource {
	source := &GCPSource{
		a: &assetClient{
			config: gcpAssetConfig{Parent: "projects/test-project"},
		},
		p: &pubSubClient{},
	}

	source.a.c.Store(fakeClient)
	return source
}

func (s *fakeAssetServiceServer) ListAssets(ctx context.Context, req *assetpb.ListAssetsRequest) (*assetpb.ListAssetsResponse, error) {
	return &assetpb.ListAssetsResponse{Assets: s.assets}, nil
}

func TestStartSyncProcessClient_Success(t *testing.T) {
	t.Parallel()

	typesToSync := []string{"storage.googleapis.com/Bucket", "compute.googleapis.com/Network"}
	fakeAssets := []*assetpb.Asset{
		{
			Name:      "//storage.googleapis.com/my-custom-bucket",
			AssetType: "storage.googleapis.com/Bucket",
			Resource: &assetpb.Resource{
				Data: &structpb.Struct{
					Fields: map[string]*structpb.Value{
						"id":   structpb.NewStringValue("my-custom-bucket"),
						"name": structpb.NewStringValue("my-custom-bucket"),
						"labels": structpb.NewStructValue(&structpb.Struct{
							Fields: map[string]*structpb.Value{
								"label1": structpb.NewStringValue("value1"),
								"label2": structpb.NewStringValue("value2"),
							},
						}),
					},
				},
			},
		},
	}
	filteredFakeAssets := filterFakeAssetsByTypes(fakeAssets, typesToSync)

	fakeClient, cleanup := newFakeAssetClient(t, filteredFakeAssets)
	defer cleanup()

	gcpInstance := setupGCPInstance(fakeClient)

	results := make(chan source.SourceData, 10)

	err := gcpInstance.StartSyncProcess(t.Context(), typesToSync, results)
	require.NoError(t, err)

	close(results)
	assetMap := assetToMap(filteredFakeAssets[0])
	for result := range results {
		assert.NotNil(t, result.Values)
		assert.Equal(t, filteredFakeAssets[0].GetAssetType(), result.Type)
		assert.Equal(t, source.DataOperationUpsert, result.Operation)
		assert.Equal(t, assetMap, result.Values)
	}
}

func TestStartSyncProcessClient_NoAssets(t *testing.T) {
	t.Parallel()

	typesToSync := []string{"compute.googleapis.com/Network"}
	fakeAssets := []*assetpb.Asset{
		{
			Name:      "//storage.googleapis.com/my-custom-bucket",
			AssetType: "storage.googleapis.com/Bucket",
			Resource: &assetpb.Resource{
				Data: &structpb.Struct{
					Fields: map[string]*structpb.Value{
						"id":   structpb.NewStringValue("my-custom-bucket"),
						"name": structpb.NewStringValue("my-custom-bucket"),
						"labels": structpb.NewStructValue(&structpb.Struct{
							Fields: map[string]*structpb.Value{
								"label1": structpb.NewStringValue("value1"),
								"label2": structpb.NewStringValue("value2"),
							},
						}),
					},
				},
			},
		},
	}
	filteredFakeAssets := filterFakeAssetsByTypes(fakeAssets, typesToSync)

	fakeClient, cleanup := newFakeAssetClient(t, filteredFakeAssets)
	defer cleanup()

	gcpInstance := setupGCPInstance(fakeClient)

	results := make(chan source.SourceData, 10)

	err := gcpInstance.StartSyncProcess(t.Context(), typesToSync, results)
	require.NoError(t, err)

	close(results)
	assert.Empty(t, results, 0)
}

func TestStartSyncProcessClient_Success_LoadJson_Bucket(t *testing.T) {
	t.Parallel()

	typesToSync := []string{"storage.googleapis.com/Bucket"}
	fakeBucketBytes, err := os.ReadFile("testdata/sync/bucket-test.json")
	require.NoError(t, err)
	var fakeBucket *assetpb.Asset
	err = json.Unmarshal(fakeBucketBytes, &fakeBucket)
	require.NoError(t, err)

	filteredFakeAssets := filterFakeAssetsByTypes([]*assetpb.Asset{fakeBucket}, typesToSync)

	fakeClient, cleanup := newFakeAssetClient(t, filteredFakeAssets)
	defer cleanup()

	gcpInstance := setupGCPInstance(fakeClient)

	results := make(chan source.SourceData, 10)

	err = gcpInstance.StartSyncProcess(t.Context(), typesToSync, results)
	require.NoError(t, err)

	close(results)
	assetMap := assetToMap(filteredFakeAssets[0])
	for result := range results {
		assert.NotNil(t, result.Values)
		assert.Equal(t, filteredFakeAssets[0].GetAssetType(), result.Type)
		assert.Equal(t, source.DataOperationUpsert, result.Operation)
		assert.Equal(t, assetMap, result.Values)
	}
}

func TestStartSyncProcessClient_Success_LoadJson_Network(t *testing.T) {
	t.Parallel()

	typesToSync := []string{"compute.googleapis.com/Network"}
	fakeNetworkBytes, err := os.ReadFile("testdata/sync/network-test.json")
	require.NoError(t, err)
	var fakeNetwork *assetpb.Asset
	err = json.Unmarshal(fakeNetworkBytes, &fakeNetwork)
	require.NoError(t, err)

	filteredFakeAssets := filterFakeAssetsByTypes([]*assetpb.Asset{fakeNetwork}, typesToSync)

	fakeClient, cleanup := newFakeAssetClient(t, filteredFakeAssets)
	defer cleanup()

	gcpInstance := setupGCPInstance(fakeClient)

	results := make(chan source.SourceData, 10)

	err = gcpInstance.StartSyncProcess(t.Context(), typesToSync, results)
	require.NoError(t, err)

	close(results)
	assetMap := assetToMap(filteredFakeAssets[0])
	for result := range results {
		assert.NotNil(t, result.Values)
		assert.Equal(t, filteredFakeAssets[0].GetAssetType(), result.Type)
		assert.Equal(t, source.DataOperationUpsert, result.Operation)
		assert.Equal(t, assetMap, result.Values)
	}
}

func TestStartSyncProcessClient_NoAssets_LoadJson_Bucket(t *testing.T) {
	t.Parallel()

	typesToSync := []string{"compute.googleapis.com/Network"}
	fakeBucketBytes, err := os.ReadFile("testdata/sync/bucket-test.json")
	require.NoError(t, err)
	var fakeBucket *assetpb.Asset
	err = json.Unmarshal(fakeBucketBytes, &fakeBucket)
	require.NoError(t, err)

	filteredFakeAssets := filterFakeAssetsByTypes([]*assetpb.Asset{fakeBucket}, typesToSync)

	fakeClient, cleanup := newFakeAssetClient(t, filteredFakeAssets)
	defer cleanup()

	gcpInstance := setupGCPInstance(fakeClient)

	results := make(chan source.SourceData, 10)

	err = gcpInstance.StartSyncProcess(t.Context(), typesToSync, results)
	require.NoError(t, err)

	close(results)
	assert.Empty(t, results, 0)
}

func TestStartSyncProcessClient_NoAssets_LoadJson_Network(t *testing.T) {
	t.Parallel()

	typesToSync := []string{"storage.googleapis.com/Bucket"}
	fakeNetworkBytes, err := os.ReadFile("testdata/sync/network-test.json")
	require.NoError(t, err)
	var fakeNetwork *assetpb.Asset
	err = json.Unmarshal(fakeNetworkBytes, &fakeNetwork)
	require.NoError(t, err)
	filteredFakeAssets := filterFakeAssetsByTypes([]*assetpb.Asset{fakeNetwork}, typesToSync)

	fakeClient, cleanup := newFakeAssetClient(t, filteredFakeAssets)
	defer cleanup()

	gcpInstance := setupGCPInstance(fakeClient)

	results := make(chan source.SourceData, 10)

	err = gcpInstance.StartSyncProcess(t.Context(), typesToSync, results)
	require.NoError(t, err)

	close(results)
	assert.Empty(t, results, 0)
}
