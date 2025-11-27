// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package gcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"cloud.google.com/go/pubsub/v2"
	"cloud.google.com/go/pubsub/v2/apiv1/pubsubpb"
	"cloud.google.com/go/pubsub/v2/pstest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/mia-platform/ibdm/internal/source"
)

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
	return c.Subscriber(pbs.GetName())
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

func setupInstancesForEventStreamTest(t *testing.T, config GCPPubSubConfig, client *pubsub.Client) *GCPInstance {
	t.Helper()
	pubSubInstance := &gcpPubSubInstance{
		config: config,
		c:      client,
	}
	gcpInstance := &GCPInstance{
		a: &gcpAssetInstance{},
		p: pubSubInstance,
	}
	return gcpInstance
}

func setupPayloadMapForEventStreamTest(t *testing.T, payload []byte) map[string]any {
	t.Helper()
	var payloadEvent GCPEvent
	err := json.Unmarshal(payload, &payloadEvent)
	require.NoError(t, err)

	var payloadMap map[string]any
	err = json.Unmarshal(payload, &payloadMap)
	require.NoError(t, err)

	var resourceName string

	if payloadEvent.Operation() == source.DataOperationDelete {
		resourceName = "priorAsset"
	} else {
		resourceName = "asset"
	}

	payloadMapResource, ok := payloadMap[resourceName].(map[string]any)
	require.True(t, ok)

	return payloadMapResource
}

func TestStartEventStream_UpsertEventStreamed(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)

	bucketModifyEventJSONPath := "testdata/event/gcp-bucket-modify.json"
	typeToStream := []string{"storage.googleapis.com/Bucket"}
	config := GCPPubSubConfig{
		ProjectID:      "test-project",
		TopicName:      "mia-platform-resources-export",
		SubscriptionID: "mia-platform-resources-export-subscription",
	}

	topicName := fmt.Sprintf("projects/%s/topics/%s", config.ProjectID, config.TopicName)
	subscriptionName := fmt.Sprintf("projects/%s/subscriptions/%s", config.ProjectID, config.SubscriptionID)

	payload, err := os.ReadFile(bucketModifyEventJSONPath)
	require.NoError(t, err)

	srv, client, _, cleanup := newFakePubSubClient(t, config, topicName, subscriptionName)

	srv.Publish(topicName, payload, nil)

	gcpInstance := setupInstancesForEventStreamTest(t, config, client)

	results := make(chan source.SourceData, 1)

	go func() {
		if err := gcpInstance.StartEventStream(ctx, typeToStream, results); err != nil {
			t.Logf("StartEventStream returned error: %v", err)
		}
	}()

	payloadMapResource := setupPayloadMapForEventStreamTest(t, payload)

	select {
	case res := <-results:
		assert.NotNil(t, res.Values)
		assert.Equal(t, payloadMapResource, res.Values)
	case <-ctx.Done():
		t.Fatalf("timeout waiting for event")
	}

	defer func() {
		cleanup()
		cancel()
		gcpInstance.Close(ctx)
	}()
}

func TestStartEventStream_DeleteEventStreamed(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)

	bucketDeleteEventJSONPath := "testdata/event/gcp-bucket-delete.json"
	typeToStream := []string{"storage.googleapis.com/Bucket"}
	config := GCPPubSubConfig{
		ProjectID:      "test-project",
		TopicName:      "mia-platform-resources-export",
		SubscriptionID: "mia-platform-resources-export-subscription",
	}

	topicName := fmt.Sprintf("projects/%s/topics/%s", config.ProjectID, config.TopicName)
	subscriptionName := fmt.Sprintf("projects/%s/subscriptions/%s", config.ProjectID, config.SubscriptionID)

	payload, err := os.ReadFile(bucketDeleteEventJSONPath)
	require.NoError(t, err)

	srv, client, _, cleanup := newFakePubSubClient(t, config, topicName, subscriptionName)

	srv.Publish(topicName, payload, nil)

	gcpInstance := setupInstancesForEventStreamTest(t, config, client)

	results := make(chan source.SourceData, 1)

	go func() {
		if err := gcpInstance.StartEventStream(ctx, typeToStream, results); err != nil {
			t.Logf("StartEventStream returned error: %v", err)
		}
	}()

	payloadMapResource := setupPayloadMapForEventStreamTest(t, payload)

	select {
	case res := <-results:
		assert.NotNil(t, res.Values)
		assert.Equal(t, payloadMapResource, res.Values)
	case <-ctx.Done():
		t.Fatalf("timeout waiting for event")
	}

	defer func() {
		cleanup()
		cancel()
		gcpInstance.Close(ctx)
	}()
}

func TestStartEventStream_NoEvents_UpsertCase(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)

	bucketModifyEventJSONPath := "testdata/event/gcp-bucket-modify.json"
	typeToStream := []string{"compute.googleapis.com/Network"}
	config := GCPPubSubConfig{
		ProjectID:      "test-project",
		TopicName:      "mia-platform-resources-export",
		SubscriptionID: "mia-platform-resources-export-subscription",
	}

	topicName := fmt.Sprintf("projects/%s/topics/%s", config.ProjectID, config.TopicName)
	subscriptionName := fmt.Sprintf("projects/%s/subscriptions/%s", config.ProjectID, config.SubscriptionID)

	payload, err := os.ReadFile(bucketModifyEventJSONPath)
	require.NoError(t, err)

	srv, client, _, cleanup := newFakePubSubClient(t, config, topicName, subscriptionName)

	srv.Publish(topicName, payload, nil)

	gcpInstance := setupInstancesForEventStreamTest(t, config, client)

	results := make(chan source.SourceData, 1)

	go func() {
		if err := gcpInstance.StartEventStream(ctx, typeToStream, results); err != nil {
			t.Logf("StartEventStream returned error: %v", err)
		}
	}()

	assert.Empty(t, results, 0)

	defer func() {
		cleanup()
		cancel()
		gcpInstance.Close(ctx)
	}()
}

func TestStartEventStream_NoEvents_DeleteCase(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)

	bucketDeleteEventJSONPath := "testdata/event/gcp-bucket-delete.json"
	typeToStream := []string{"compute.googleapis.com/Network"}
	config := GCPPubSubConfig{
		ProjectID:      "test-project",
		TopicName:      "mia-platform-resources-export",
		SubscriptionID: "mia-platform-resources-export-subscription",
	}

	topicName := fmt.Sprintf("projects/%s/topics/%s", config.ProjectID, config.TopicName)
	subscriptionName := fmt.Sprintf("projects/%s/subscriptions/%s", config.ProjectID, config.SubscriptionID)

	payload, err := os.ReadFile(bucketDeleteEventJSONPath)
	require.NoError(t, err)

	srv, client, _, cleanup := newFakePubSubClient(t, config, topicName, subscriptionName)

	srv.Publish(topicName, payload, nil)

	gcpInstance := setupInstancesForEventStreamTest(t, config, client)

	results := make(chan source.SourceData, 1)

	go func() {
		if err := gcpInstance.StartEventStream(ctx, typeToStream, results); err != nil {
			t.Logf("StartEventStream returned error: %v", err)
		}
	}()

	assert.Empty(t, results, 0)

	defer func() {
		cleanup()
		cancel()
		gcpInstance.Close(ctx)
	}()
}
