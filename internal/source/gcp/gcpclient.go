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
	errorsList := make([]error, 0)
	assetInstance, err := newGCPAssetInstance()
	if err != nil {
		errorsList = append(errorsList, err)
	}
	pubSubInstance, err := newGCPPubSubInstance()
	if err != nil {
		errorsList = append(errorsList, err)
	}
	if len(errorsList) > 0 {
		return nil, errors.Join(errorsList...)
	}
	return &GCPInstance{
		a: assetInstance,
		p: pubSubInstance,
	}, nil
}

func newGCPPubSubInstance() (*gcpPubSubInstance, error) {
	envVars, err := env.ParseAs[GCPPubSubEnvironmentVariables]()
	if err != nil {
		return nil, err
	}
	if err := checkPubSubEnvVariables(envVars); err != nil {
		return nil, err
	}
	return &gcpPubSubInstance{
		config: GCPPubSubConfig(envVars),
	}, nil
}

func newGCPAssetInstance() (*gcpAssetInstance, error) {
	envVars, err := env.ParseAs[GCPAssetEnvironmentVariables]()
	if err != nil {
		return nil, err
	}
	if err := checkAssetEnvVariables(envVars); err != nil {
		return nil, err
	}
	return &gcpAssetInstance{
		config: GCPAssetConfig(envVars),
	}, nil
}

func (p *gcpPubSubInstance) initPubSubClient(ctx context.Context) error {
	if p.c != nil {
		return nil
	}
	client, err := pubsub.NewClient(ctx, p.config.ProjectID)
	if err != nil {
		return err
	}
	p.c = client
	return nil
}

func (p *gcpPubSubInstance) initPubSubSubscriber(log logger.Logger) *pubsub.Subscriber {
	log.Debug("starting to listen to Pub/Sub messages",
		"projectId", p.config.ProjectID,
		"topicName", p.config.TopicName,
		"subscriptionId", p.config.SubscriptionID,
	)
	return p.c.Subscriber(p.config.SubscriptionID)
}

func (a *gcpAssetInstance) initAssetClient(ctx context.Context) error {
	if a.c != nil {
		return nil
	}
	client, err := asset.NewClient(ctx)
	if err != nil {
		return err
	}
	a.c = client
	return nil
}

func (p *gcpPubSubInstance) closePubSubClient() error {
	if p.c != nil {
		if err := p.c.Close(); err != nil {
			return err
		}
		p.c = nil
	}
	return nil
}

func (a *gcpAssetInstance) closeAssetClient() error {
	if a.c != nil {
		if err := a.c.Close(); err != nil {
			return err
		}
		a.c = nil
	}
	return nil
}

func (g *GCPInstance) Close(ctx context.Context) error {
	errorsList := make([]error, 0)
	err := g.p.closePubSubClient()
	if err != nil {
		errorsList = append(errorsList, err)
	}
	err = g.a.closeAssetClient()
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

func eventAssetToMap(asset GCPEventAsset) map[string]any {
	b, err := json.Marshal(asset)
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil
	}
	return out
}

func (a *gcpAssetInstance) getListAssetsRequest(typesToSync []string) *assetpb.ListAssetsRequest {
	return &assetpb.ListAssetsRequest{
		Parent:      "projects/" + a.config.ProjectID,
		AssetTypes:  typesToSync,
		ContentType: assetpb.ContentType_RESOURCE,
	}
}

func (g *GCPInstance) StartSyncProcess(ctx context.Context, typesToSync []string, results chan<- source.SourceData) error {
	log := logger.FromContext(ctx).WithName(loggerName)
	err := g.a.initAssetClient(ctx)
	if err != nil {
		log.Error("failed to initialize Asset client", "error", err)
		return err
	}
	defer func() {
		if err := g.a.closeAssetClient(); err != nil {
			log.Error("failed to close Asset client", "error", err)
		}
	}()

	req := g.a.getListAssetsRequest(typesToSync)

	it := g.a.c.ListAssets(ctx, req)

	for {
		asset, err := it.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		results <- source.SourceData{
			Type:      asset.GetAssetType(),
			Operation: source.DataOperationUpsert,
			Values:    assetToMap(asset),
		}
	}
	return nil
}

func (g *GCPInstance) StartEventStream(ctx context.Context, typesToStream []string, results chan<- source.SourceData) error {
	log := logger.FromContext(ctx).WithName(loggerName)
	err := g.p.initPubSubClient(ctx)
	if err != nil {
		log.Error("failed to initialize Pub/Sub client", "error", err)
		return err
	}
	defer func() {
		if err = g.p.closePubSubClient(); err != nil {
			log.Error("failed to close Pub/Sub client", "error", err)
		}
	}()

	return g.p.gcpListener(ctx, log, typesToStream, results)
}

func (p *gcpPubSubInstance) gcpListener(ctx context.Context, log logger.Logger, typesToStream []string, results chan<- source.SourceData) error {
	subscriber := p.initPubSubSubscriber(log)
	err := subscriber.Receive(ctx, func(_ context.Context, msg *pubsub.Message) {
		event, err := gcpListenerHandler(msg.Data)
		if err != nil {
			// TODO: manage to create the subscription if does not exist
			log.Error("failed to handle Pub/Sub message", "messageId", msg.ID)
			msg.Nack()
			return
		}
		if !event.IsTypeIn(typesToStream) {
			log.Debug("skipping event of unrequested type",
				"messageId", msg.ID,
				"eventType", event.GetAssetType(),
			)
			msg.Ack()
			return
		}
		var valuesMap map[string]any
		if event.Operation() == source.DataOperationDelete {
			valuesMap = eventAssetToMap(event.GetPriorAsset())
		} else {
			valuesMap = eventAssetToMap(event.GetAsset())
		}
		if valuesMap == nil {
			log.Error("failed to convert asset to map",
				"messageId", msg.ID,
				"eventType", event.GetAssetType(),
			)
			msg.Nack()
			return
		}
		results <- source.SourceData{
			Type:      event.GetAssetType(),
			Operation: event.Operation(),
			Values:    valuesMap,
		}
		msg.Ack()
	})
	if err != nil {
		return fmt.Errorf("got error in Receive: %w", err)
	}
	return nil
}

func gcpListenerHandler(data []byte) (*GCPEvent, error) {
	var event *GCPEvent
	if err := json.Unmarshal(data, &event); err != nil {
		return nil, err
	}
	return event, nil
}
