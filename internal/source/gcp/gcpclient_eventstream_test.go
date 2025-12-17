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

const (
	testTopicTemplate = "projects/%s/topics/topic-name"
)

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

func newFakePubSubClient(t *testing.T, config pubSubConfig, topicName string, subscriptionName string) (*pstest.Server, *pubsub.Client, func()) {
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
	mustCreateSubConfig(t, client, &pubsubpb.Subscription{
		Name:               subscriptionName,
		Topic:              topicName,
		AckDeadlineSeconds: int32(15),
	})

	return srv, client, func() {
		srv.Close()
		conn.Close()
		client.Close()
	}
}

func setupInstancesForEventStreamTest(t *testing.T, config pubSubConfig, client *pubsub.Client) *Source {
	t.Helper()
	pubSubClient := &pubSubClient{
		config: config,
	}
	pubSubClient.c.Store(client)

	gcpInstance := &Source{
		a: &assetClient{},
		p: pubSubClient,
	}
	return gcpInstance
}

func TestStartEventStream_UpsertEventStreamed(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
	defer cancel()

	bucketModifyEventJSONPath := "testdata/event/original/message-gcp-bucket-modify.json"
	bucketModifyPayloadJSONPath := "testdata/event/expected/payload-gcp-bucket-modify.json"
	typeToStream := []string{"storage.googleapis.com/Bucket"}
	config := pubSubConfig{
		ProjectID:      "test-project",
		SubscriptionID: "subscription-id",
	}

	topicName := fmt.Sprintf(testTopicTemplate, config.ProjectID)
	subscriptionName := fmt.Sprintf("projects/%s/subscriptions/%s", config.ProjectID, config.SubscriptionID)

	payload, err := os.ReadFile(bucketModifyEventJSONPath)
	require.NoError(t, err)

	payloadMap, err := os.ReadFile(bucketModifyPayloadJSONPath)
	require.NoError(t, err)
	var payloadMapResource map[string]any
	err = json.Unmarshal(payloadMap, &payloadMapResource)
	require.NoError(t, err)

	srv, client, cleanup := newFakePubSubClient(t, config, topicName, subscriptionName)
	defer cleanup()

	srv.Publish(topicName, payload, nil)

	gcpInstance := setupInstancesForEventStreamTest(t, config, client)

	results := make(chan source.Data)

	closeChannel := make(chan struct{})
	go func() {
		if err := gcpInstance.StartEventStream(ctx, typeToStream, results); err != nil {
			assert.ErrorIs(t, err, ErrGCPSource)
			assert.ErrorContains(t, err, "the client connection is closing")
			close(closeChannel)
		}
	}()

	select {
	case res := <-results:
		assert.NotNil(t, res.Values)
		assert.Equal(t, payloadMapResource, res.Values)
	case <-ctx.Done():
		require.Fail(t, "timeout waiting for event")
	}

	gcpInstance.Close(ctx)
	<-closeChannel
}

func TestStartEventStream_DeleteEventStreamed(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
	defer cancel()

	bucketDeleteEventJSONPath := "testdata/event/original/message-gcp-bucket-delete.json"
	bucketDeletePayloadJSONPath := "testdata/event/expected/payload-gcp-bucket-delete.json"
	typeToStream := []string{"storage.googleapis.com/Bucket"}
	config := pubSubConfig{
		ProjectID:      "test-project",
		SubscriptionID: "subscription-id",
	}

	topicName := fmt.Sprintf(testTopicTemplate, config.ProjectID)
	subscriptionName := fmt.Sprintf("projects/%s/subscriptions/%s", config.ProjectID, config.SubscriptionID)

	payload, err := os.ReadFile(bucketDeleteEventJSONPath)
	require.NoError(t, err)

	payloadMap, err := os.ReadFile(bucketDeletePayloadJSONPath)
	require.NoError(t, err)
	var payloadMapResource map[string]any
	err = json.Unmarshal(payloadMap, &payloadMapResource)
	require.NoError(t, err)

	srv, client, cleanup := newFakePubSubClient(t, config, topicName, subscriptionName)
	defer cleanup()

	srv.Publish(topicName, payload, nil)

	gcpInstance := setupInstancesForEventStreamTest(t, config, client)

	results := make(chan source.Data)

	closeChannel := make(chan struct{})
	go func() {
		if err := gcpInstance.StartEventStream(ctx, typeToStream, results); err != nil {
			assert.ErrorIs(t, err, ErrGCPSource)
			assert.ErrorContains(t, err, "the client connection is closing")
			close(closeChannel)
		}
	}()

	select {
	case res := <-results:
		assert.NotNil(t, res.Values)
		assert.Equal(t, payloadMapResource, res.Values)
	case <-ctx.Done():
		require.Fail(t, "timeout waiting for event")
	}

	gcpInstance.Close(ctx)
	<-closeChannel
}

func TestStartEventStream_NoEvents_UpsertCase(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
	defer cancel()

	bucketModifyEventJSONPath := "testdata/event/original/message-gcp-bucket-modify.json"
	typeToStream := []string{"compute.googleapis.com/Network"}
	config := pubSubConfig{
		ProjectID:      "test-project",
		SubscriptionID: "subscription-id",
	}

	topicName := fmt.Sprintf(testTopicTemplate, config.ProjectID)
	subscriptionName := fmt.Sprintf("projects/%s/subscriptions/%s", config.ProjectID, config.SubscriptionID)

	payload, err := os.ReadFile(bucketModifyEventJSONPath)
	require.NoError(t, err)

	srv, client, cleanup := newFakePubSubClient(t, config, topicName, subscriptionName)
	defer cleanup()

	msgID := srv.Publish(topicName, payload, nil)
	gcpInstance := setupInstancesForEventStreamTest(t, config, client)
	results := make(chan source.Data)

	closeChannel := make(chan struct{})
	go func() {
		if err := gcpInstance.StartEventStream(ctx, typeToStream, results); err != nil {
			assert.ErrorIs(t, err, ErrGCPSource)
			assert.ErrorContains(t, err, "the client connection is closing")
		}

		close(closeChannel)
	}()

	for {
		message := srv.Message(msgID)
		if message.Acks > 0 {
			break
		}
	}

	gcpInstance.Close(ctx)
	<-closeChannel
	assert.Empty(t, results, 0)
}

func TestStartEventStream_NoEvents_DeleteCase(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
	defer cancel()

	bucketDeleteEventJSONPath := "testdata/event/original/message-gcp-bucket-delete.json"
	typeToStream := []string{"compute.googleapis.com/Network"}
	config := pubSubConfig{
		ProjectID:      "test-project",
		SubscriptionID: "subscription-id",
	}

	topicName := fmt.Sprintf(testTopicTemplate, config.ProjectID)
	subscriptionName := fmt.Sprintf("projects/%s/subscriptions/%s", config.ProjectID, config.SubscriptionID)

	payload, err := os.ReadFile(bucketDeleteEventJSONPath)
	require.NoError(t, err)

	srv, client, cleanup := newFakePubSubClient(t, config, topicName, subscriptionName)
	defer cleanup()

	msgID := srv.Publish(topicName, payload, nil)
	gcpInstance := setupInstancesForEventStreamTest(t, config, client)
	results := make(chan source.Data)

	closeChannel := make(chan struct{})
	go func() {
		if err := gcpInstance.StartEventStream(ctx, typeToStream, results); err != nil {
			assert.ErrorIs(t, err, ErrGCPSource)
			assert.ErrorContains(t, err, "the client connection is closing")
		}

		close(closeChannel)
	}()

	for {
		message := srv.Message(msgID)
		if message.Acks > 0 {
			break
		}
	}

	gcpInstance.Close(ctx)
	<-closeChannel
	assert.Empty(t, results, 0)
}
