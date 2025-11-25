// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package gcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/caarlos0/env/v11"

	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/mia-platform/ibdm/internal/source"

	"google.golang.org/api/iterator"
	"google.golang.org/protobuf/encoding/protojson"

	asset "cloud.google.com/go/asset/apiv1"
	"cloud.google.com/go/asset/apiv1/assetpb"
	"cloud.google.com/go/pubsub/v2"
)

const (
	loggerName = "ibdm:source:gcp"
)

var (
	ErrMalformedEvent = errors.New("malformed event")
)

// TODO: add mutex TryLock management for StartEventStream and StartSyncProcess to manage concurrency

func checkPubSubEnvVariables(cfg GCPPubSubEnvironmentVariables) error {
	errorsList := make([]error, 0)
	if cfg.ProjectID == "" {
		errorsList = append(errorsList, errors.New("GCP_PROJECT_ID environment variable is required"))
	}
	if cfg.TopicName == "" {
		errorsList = append(errorsList, errors.New("GCP_TOPIC_NAME environment variable is required"))
	}
	if cfg.SubscriptionID == "" {
		errorsList = append(errorsList, errors.New("GCP_SUBSCRIPTION_ID environment variable is required"))
	}
	if len(errorsList) > 0 {
		return errors.Join(errorsList...)
	}
	return nil
}

func checkAssetEnvVariables(cfg GCPAssetEnvironmentVariables) error {
	if cfg.ProjectID == "" {
		return errors.New("GCP_PROJECT_ID environment variable is required")
	}
	return nil
}

func NewGCPInstance(ctx context.Context) (*GCPInstance, error) {
	log := logger.FromContext(ctx).WithName(loggerName)
	assetInstance, err := newGCPAssetInstance(ctx)
	if err != nil {
		return nil, err
	}
	pubSubInstance, err := newGCPPubSubInstance(ctx)
	if err != nil {
		return nil, err
	}
	return &GCPInstance{
		a:   assetInstance,
		p:   pubSubInstance,
		log: log,
	}, nil
}

func newGCPPubSubInstance(ctx context.Context) (*gcpPubSubInstance, error) {
	envVars, err := env.ParseAs[GCPPubSubEnvironmentVariables]()
	if err != nil {
		return nil, err
	}
	if err := checkPubSubEnvVariables(envVars); err != nil {
		return nil, err
	}
	return &gcpPubSubInstance{
		config: GCPPubSubConfig{
			ProjectID:      envVars.ProjectID,
			TopicName:      envVars.TopicName,
			SubscriptionID: envVars.SubscriptionID,
		},
	}, nil
}

func newGCPAssetInstance(ctx context.Context) (*gcpAssetInstance, error) {
	envVars, err := env.ParseAs[GCPAssetEnvironmentVariables]()
	if err != nil {
		return nil, err
	}
	if err := checkAssetEnvVariables(envVars); err != nil {
		return nil, err
	}
	return &gcpAssetInstance{
		config: GCPAssetConfig{
			ProjectID: envVars.ProjectID,
		},
	}, nil
}

func (g *GCPInstance) initPubSubClient(ctx context.Context) error {
	client, err := pubsub.NewClient(ctx, g.p.config.ProjectID)
	if err != nil {
		return err
	}
	g.p.c = client
	return nil
}

func (g *GCPInstance) initPubSubSubscriber(ctx context.Context) error {
	g.p.s = g.p.c.Subscriber(g.p.config.SubscriptionID)
	g.log.Debug("starting to listen to Pub/Sub messages",
		"projectId", g.p.config.ProjectID,
		"topicName", g.p.config.TopicName,
		"subscriptionId", g.p.config.SubscriptionID,
	)
	if g.p.s == nil {
		return fmt.Errorf("failed to create Pub/Sub subscriber for subscription %s", g.p.config.SubscriptionID)
	}
	return nil
}

func (g *GCPInstance) initAssetClient(ctx context.Context) error {
	client, err := asset.NewClient(ctx)
	if err != nil {
		return err
	}
	g.a.c = client
	return nil
}

func (p *gcpPubSubInstance) closePubSubClient(ctx context.Context) error {
	if p.c != nil {
		if err := p.c.Close(); err != nil {
			return err
		}
		p.c = nil
	}
	return nil
}

func (p *gcpAssetInstance) closeAssetClient(ctx context.Context) error {
	if p.c != nil {
		if err := p.c.Close(); err != nil {
			return err
		}
		p.c = nil
	}
	return nil
}

func (g *GCPInstance) Close(ctx context.Context) error {
	errorsList := make([]error, 0)
	err := g.p.closePubSubClient(ctx)
	if err != nil {
		errorsList = append(errorsList, err)
	}
	err = g.a.closeAssetClient(ctx)
	if err != nil {
		errorsList = append(errorsList, err)
	}
	if len(errorsList) > 0 {
		return errors.Join(errorsList...)
	}
	return nil
}

func assetToMap(asset *assetpb.Asset) map[string]any {
	if asset == nil {
		return nil
	}
	b, err := protojson.Marshal(asset)
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil
	}
	return out
}

func (g *GCPInstance) StartSyncProcess(ctx context.Context, typesToSync []string, results chan<- source.SourceData) (err error) {
	if g.a.c == nil {
		g.initAssetClient(ctx)
	}
	defer func() {
		g.a.closeAssetClient(ctx)
	}()
	req := &assetpb.ListAssetsRequest{
		Parent:      "projects/" + g.a.config.ProjectID,
		AssetTypes:  typesToSync,
		ContentType: assetpb.ContentType_RESOURCE,
	}

	it := g.a.c.ListAssets(ctx, req)

	for {
		asset, err := it.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		results <- source.SourceData{
			Type:      asset.AssetType,
			Operation: source.DataOperationUpsert,
			Values:    assetToMap(asset),
		}
	}

	return nil
}

func (g *GCPInstance) StartEventStream(ctx context.Context, typesToStream []string, results chan<- source.SourceData) (err error) {
	if g.p.c == nil {
		g.initPubSubClient(ctx)
	}
	defer g.Close(ctx)

	return g.gcpListener(ctx, results)
}

func (g *GCPInstance) gcpListener(ctx context.Context, results chan<- source.SourceData) (err error) {
	g.initPubSubSubscriber(ctx)
	err = g.p.s.Receive(ctx, func(_ context.Context, msg *pubsub.Message) {
		event, err := gcpListenerHandler(ctx, msg.Data)
		if err != nil {
			g.log.Error("failed to handle Pub/Sub message",
				"messageId", msg.ID,
			)
			msg.Nack()
			return
		}
		results <- source.SourceData{
			Type:      event.AssetType(),
			Operation: event.Operation(),
			Values:    event.Resource(),
		}
		msg.Ack()
	})
	if err != nil {
		return fmt.Errorf("got error in Receive: %w", err)
	}
	return nil
}

func gcpListenerHandler(ctx context.Context, data []byte) (event *GCPEvent, err error) {
	fmt.Println("message data:", string(data))
	if err = json.Unmarshal(data, &event); err != nil {
		fmt.Printf("malformed event: %s\n", err.Error())
		return nil, err
	}
	return event, nil
}
