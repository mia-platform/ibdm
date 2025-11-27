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
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/source"
)

type fakeAssetServiceServer struct {
	assetpb.UnimplementedAssetServiceServer

	assets []*assetpb.Asset
}

var fakeAssets = []*assetpb.Asset{
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
	{
		Name:      "//compute.googleapis.com/my-custom-network",
		AssetType: "compute.googleapis.com/Network",
		Resource: &assetpb.Resource{
			Data: &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"id":   structpb.NewStringValue("my-custom-network"),
					"name": structpb.NewStringValue("my-custom-network"),
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

func newFakeAssetClient(ctx context.Context, fakeAssets []*assetpb.Asset) (*asset.Client, *grpc.Server, net.Listener, error) {
	fakeSrv := &fakeAssetServiceServer{}
	fakeSrv.assets = fakeAssets
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, nil, nil, err
	}
	gsrv := grpc.NewServer()
	assetpb.RegisterAssetServiceServer(gsrv, fakeSrv)
	fakeServerAddr := l.Addr().String()
	go func() {
		if err := gsrv.Serve(l); err != nil {
			panic(err)
		}
	}()

	time.Sleep(10 * time.Millisecond)

	client, err := asset.NewClient(ctx,
		option.WithEndpoint(fakeServerAddr),
		option.WithoutAuthentication(),
		option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
	)
	return client, gsrv, l, err
}

func (s *fakeAssetServiceServer) ListAssets(ctx context.Context, req *assetpb.ListAssetsRequest) (*assetpb.ListAssetsResponse, error) {
	return &assetpb.ListAssetsResponse{Assets: s.assets}, nil
}

func singleTestStartSyncProcess(t *testing.T, typesToSync []string, fakeAssets []*assetpb.Asset, empty bool) {
	ctx := t.Context()
	filteredFakeAssets := filterFakeAssetsByTypes(fakeAssets, typesToSync)

	fakeClient, gsrv, l, err := newFakeAssetClient(ctx, filteredFakeAssets)
	if err != nil {
		gsrv.Stop()
		t.Fatalf("failed to create fake asset client: %v", err)
	}
	defer func() {
		_ = fakeClient.Close()
		gsrv.Stop()
		_ = l.Close()
	}()

	gi := &GCPInstance{
		a: &gcpAssetInstance{
			config: GCPAssetConfig{ProjectID: "test-project"},
			c:      fakeClient,
		},
		p: &gcpPubSubInstance{},
	}

	results := make(chan source.SourceData, 10)

	err = gi.StartSyncProcess(ctx, typesToSync, results)
	require.NoError(t, err)

	close(results)
	if empty {
		assert.Empty(t, results, 0)
	} else {
		assetMap := assetToMap(filteredFakeAssets[0])
		for result := range results {
			assert.NotNil(t, result.Values)
			assert.Equal(t, filteredFakeAssets[0].GetAssetType(), result.Type)
			assert.Equal(t, source.DataOperationUpsert, result.Operation)
			assert.Equal(t, assetMap, result.Values)
		}
	}
}

func TestStartSyncProcessClient_Success(t *testing.T) {
	typesToSync := []string{"storage.googleapis.com/Bucket", "compute.googleapis.com/Network"}
	fakeAssets = []*assetpb.Asset{
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
	singleTestStartSyncProcess(t, typesToSync, fakeAssets, false)
}

func TestStartSyncProcessClient_NoAssets(t *testing.T) {
	typesToSync := []string{"compute.googleapis.com/Network"}
	fakeAssets = []*assetpb.Asset{
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
	singleTestStartSyncProcess(t, typesToSync, fakeAssets, true)
}

func TestStartSyncProcessClient_SuccessLoadJson(t *testing.T) {
	typesToSync := []string{"storage.googleapis.com/Bucket"}
	fakeBucketBytes, err := os.ReadFile("testdata/sync/bucket-test.json")
	require.NoError(t, err)
	var fakeBucket *assetpb.Asset
	err = json.Unmarshal(fakeBucketBytes, &fakeBucket)
	require.NoError(t, err)
	singleTestStartSyncProcess(t, typesToSync, []*assetpb.Asset{fakeBucket}, false)

	typesToSync = []string{"compute.googleapis.com/Network"}
	fakeNetworkBytes, err := os.ReadFile("testdata/sync/network-test.json")
	require.NoError(t, err)
	var fakeNetwork *assetpb.Asset
	err = json.Unmarshal(fakeNetworkBytes, &fakeNetwork)
	require.NoError(t, err)
	singleTestStartSyncProcess(t, typesToSync, []*assetpb.Asset{fakeNetwork}, false)
}

func TestStartSyncProcessClient_NoAssetsLoadJson(t *testing.T) {
	typesToSync := []string{"compute.googleapis.com/Network"}
	fakeBucketBytes, err := os.ReadFile("testdata/sync/bucket-test.json")
	require.NoError(t, err)
	var fakeBucket *assetpb.Asset
	err = json.Unmarshal(fakeBucketBytes, &fakeBucket)
	require.NoError(t, err)
	singleTestStartSyncProcess(t, typesToSync, []*assetpb.Asset{fakeBucket}, true)

	typesToSync = []string{"storage.googleapis.com/Bucket"}
	fakeNetworkBytes, err := os.ReadFile("testdata/sync/network-test.json")
	require.NoError(t, err)
	var fakeNetwork *assetpb.Asset
	err = json.Unmarshal(fakeNetworkBytes, &fakeNetwork)
	require.NoError(t, err)
	singleTestStartSyncProcess(t, typesToSync, []*assetpb.Asset{fakeNetwork}, true)
}
