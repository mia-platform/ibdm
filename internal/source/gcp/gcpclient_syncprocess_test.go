// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package gcp

import (
	"context"
	"net"
	"testing"
	"time"

	asset "cloud.google.com/go/asset/apiv1"
	assetpb "cloud.google.com/go/asset/apiv1/assetpb"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/stretchr/testify/assert"

	"github.com/mia-platform/ibdm/internal/source"
)

type fakeAssetServiceServer struct {
	assetpb.UnimplementedAssetServiceServer
}

func newFakeAssetClient(ctx context.Context) (*asset.Client, *grpc.Server, net.Listener, error) {
	fakeSrv := &fakeAssetServiceServer{}
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
	assets := []*assetpb.Asset{
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
	return &assetpb.ListAssetsResponse{Assets: assets}, nil
}

func TestListAvailableAssets(t *testing.T) {
	ctx := t.Context()

	client, gsrv, l, err := newFakeAssetClient(ctx)

	if err != nil {
		gsrv.Stop()
		t.Fatalf("failed to create asset client: %v", err)
	}
	defer func() {
		_ = client.Close()
		gsrv.Stop()
		_ = l.Close()
	}()

	req := &assetpb.ListAssetsRequest{
		Parent:      "projects/test-project",
		AssetTypes:  []string{"storage.googleapis.com/Bucket", "compute.googleapis.com/Network"},
		ContentType: assetpb.ContentType_RESOURCE,
	}
	it := client.ListAssets(ctx, req)
	got := make([]*assetpb.Asset, 0)
	for {
		a, err := it.Next()
		if err != nil {
			break
		}
		got = append(got, a)
	}

	if len(got) != 2 {
		t.Fatalf("expected 2 assets, got %d", len(got))
	}

	if got[0].GetName() == "" || got[1].GetName() == "" {
		t.Fatalf("returned assets have empty names")
	}
}

func TestStartSyncProcessInjectFakeClient(t *testing.T) {
	ctx := t.Context()

	fakeClient, gsrv, l, err := newFakeAssetClient(ctx)
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

	if err := gi.StartSyncProcess(ctx, []string{"storage.googleapis.com/Bucket", "compute.googleapis.com/Network"}, results); err != nil {
		t.Fatalf("StartSyncProcess returned error: %v", err)
	}

	close(results)
	for result := range results {
		assert.NotNil(t, result.Values)
		if result.Type != "storage.googleapis.com/Bucket" && result.Type != "compute.googleapis.com/Network" {
			t.Fatalf("unexpected result type: %s", result.Type)
		}
	}
}
