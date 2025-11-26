// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package gcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	asset "cloud.google.com/go/asset/apiv1"
	assetpb "cloud.google.com/go/asset/apiv1/assetpb"
	"cloud.google.com/go/pubsub/v2"
	"cloud.google.com/go/pubsub/v2/apiv1/pubsubpb"
	"cloud.google.com/go/pubsub/v2/pstest"
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
}

func newFakeAssetClient(ctx context.Context) (*asset.Client, *grpc.Server, net.Listener, error) {
	// Setup the fake server.
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

	// Ensure server is ready (best-effort).
	time.Sleep(10 * time.Millisecond)

	client, err := asset.NewClient(ctx,
		option.WithEndpoint(fakeServerAddr),
		option.WithoutAuthentication(),
		option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
	)
	return client, gsrv, l, err
}

// ListAssets implements the AssetService ListAssets RPC for the fake server.
func (s *fakeAssetServiceServer) ListAssets(ctx context.Context, req *assetpb.ListAssetsRequest) (*assetpb.ListAssetsResponse, error) {
	// Return a small set of fake assets.
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

func newFakeGCPPubSubInstance() *gcpPubSubInstance {
	return &gcpPubSubInstance{
		config: GCPPubSubConfig{
			ProjectID:      "console-infrastructure-lab",
			TopicName:      "mia-platform-resources-export",
			SubscriptionID: "mia-platform-resources-export-subscription",
		},
	}
}

func TestRealStartEventStream(t *testing.T) {
	t.Skip("Skipping real start event stream test")
	ctx := t.Context()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	pubSubInstance := newFakeGCPPubSubInstance()

	gcpInstance := &GCPInstance{
		a: &gcpAssetInstance{},
		p: pubSubInstance,
	}

	results := make(chan source.SourceData, 10)

	err := gcpInstance.StartEventStream(ctx, []string{"storage.googleapis.com/Bucket"}, results)
	if err != nil {
		t.Fatalf("StartEventStream returned error: %v", err)
	}

	close(results)
	assert.Len(t, results, 1)
	for result := range results {
		fmt.Println("type", result.Type)
		fmt.Println("type", result.Operation)
		fmt.Println("type", result.Values)
		assert.NotNil(t, result.Values)
		if result.Type != "storage.googleapis.com/Bucket" {
			t.Fatalf("unexpected result type: %s", result.Type)
		}
	}
}

func mustCreateTopic(t *testing.T, c *pubsub.Client, name string) *pubsub.Publisher {
	t.Helper()
	_, err := c.TopicAdminClient.CreateTopic(t.Context(), &pubsubpb.Topic{Name: name})
	require.NoError(t, err)
	return c.Publisher(name)
}

func mustCreateSubConfig(t *testing.T, c *pubsub.Client, pbs *pubsubpb.Subscription) *pubsub.Subscriber {
	t.Helper()
	_, err := c.SubscriptionAdminClient.CreateSubscription(t.Context(), pbs)
	require.NoError(t, err)
	return c.Subscriber(pbs.Name)
}

func newFakePubSubClient(t *testing.T, config GCPPubSubConfig, topicName string, subscriptionName string) (*pstest.Server, *pubsub.Client, *pubsub.Subscriber, func()) {
	t.Helper()
	ctx := t.Context()
	srv := pstest.NewServer()

	conn, err := grpc.NewClient(srv.Addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)

	client, err := pubsub.NewClient(ctx, config.ProjectID,
		option.WithEndpoint(srv.Addr),
		option.WithoutAuthentication(),
		option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
		option.WithTelemetryDisabled(),
	)
	require.NoError(t, err)

	mustCreateTopic(t, client, topicName)
	sub := mustCreateSubConfig(t, client, &pubsubpb.Subscription{
		Name:               subscriptionName,
		Topic:              topicName,
		AckDeadlineSeconds: int32(15),
	})

	return srv, client, sub, func() {
		srv.Close()
		conn.Close()
		client.Close()
	}
}

func TestStartEventStream(t *testing.T) {
	ctx := t.Context()

	config := GCPPubSubConfig{
		ProjectID:      "test-project",
		TopicName:      "mia-platform-resources-export",
		SubscriptionID: "mia-platform-resources-export-subscription",
	}

	topicName := fmt.Sprintf("projects/%s/topics/%s", config.ProjectID, config.TopicName)
	subscriptionName := fmt.Sprintf("projects/%s/subscriptions/%s", config.ProjectID, config.SubscriptionID)

	payload, err := os.ReadFile("testdata/gcp-bucket-event.json")
	require.NoError(t, err)

	srv, client, _, cleanup := newFakePubSubClient(t, config, topicName, subscriptionName)
	defer cleanup()

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	srv.Publish(topicName, payload, nil)

	pubSubInstance := &gcpPubSubInstance{
		config: config,
		c:      client,
	}

	gcpInstance := &GCPInstance{
		a: &gcpAssetInstance{},
		p: pubSubInstance,
	}

	results := make(chan source.SourceData, 1)

	go func() {
		if err := gcpInstance.StartEventStream(ctx, []string{"storage.googleapis.com/Bucket"}, results); err != nil {
			t.Logf("StartEventStream returned error: %v", err)
		}
	}()

	var payloadMap map[string]any
	err = json.Unmarshal(payload, &payloadMap)
	require.NoError(t, err)

	payloadMapAsset, ok := payloadMap["asset"].(map[string]any)
	require.True(t, ok)

	select {
	case res := <-results:
		assert.NotNil(t, res.Values)
		resultJson, _ := json.Marshal(res.Values)
		fmt.Println("result JSON", string(resultJson))
		assert.Equal(t, payloadMapAsset, res.Values)
		if res.Type != "storage.googleapis.com/Bucket" {
			t.Fatalf("unexpected result type: %s", res.Type)
		}
	case <-ctx.Done():
		t.Fatalf("timeout waiting for event")
	}
	defer gcpInstance.Close(ctx)
}
