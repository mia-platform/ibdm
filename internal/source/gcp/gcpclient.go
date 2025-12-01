// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package gcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	asset "cloud.google.com/go/asset/apiv1"
	"cloud.google.com/go/asset/apiv1/assetpb"
	"cloud.google.com/go/pubsub/v2"
	"github.com/caarlos0/env/v11"
	"google.golang.org/api/iterator"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/mia-platform/ibdm/internal/source"
)

const (
	loggerName = "ibdm:source:gcp"
)

var (
	ErrMissingEnvVariable     = errors.New("missing environment variable")
	ErrClosingGCPSourceClient = errors.New("error closing GCP source client")
	ErrClientInitialization   = errors.New("error initializing GCP client")
	ErrListAssetIterator      = errors.New("error iterating over ListAssets response")
	ErrReceivePubSubMessages  = errors.New("error receiving Pub/Sub messages")
)

func checkPubSubConfig(cfg GCPPubSubConfig) error {
	errorsList := make([]error, 0)
	if cfg.ProjectID == "" {
		errorsList = append(errorsList, errors.New("GOOGLE_CLOUD_PUBSUB_PROJECT environment variable is required"))
	}
	if cfg.TopicName == "" {
		errorsList = append(errorsList, errors.New("GOOGLE_CLOUD_PUBSUB_TOPIC environment variable is required"))
	}
	if cfg.SubscriptionID == "" {
		errorsList = append(errorsList, errors.New("GOOGLE_CLOUD_PUBSUB_SUBSCRIPTION environment variable is required"))
	}
	if len(errorsList) > 0 {
		err := errors.Join(errorsList...)
		return fmt.Errorf("%w: %s", ErrMissingEnvVariable, err.Error())
	}
	return nil
}

func checkAssetConfig(cfg GCPAssetConfig) error {
	if cfg.ProjectID == "" {
		return fmt.Errorf("%w: %s", ErrMissingEnvVariable, errors.New("GOOGLE_CLOUD_ASSET_PROJECT environment variable is required"))
	}
	return nil
}

func NewGCPSource(ctx context.Context) (*GCPSource, error) {
	errorsList := make([]error, 0)
	assetClient, err := newAssetClient()
	if err != nil {
		errorsList = append(errorsList, err)
	}
	pubSubClient, err := newPubSubClient()
	if err != nil {
		errorsList = append(errorsList, err)
	}
	if len(errorsList) > 0 {
		return nil, errors.Join(errorsList...)
	}
	return &GCPSource{
		a: assetClient,
		p: pubSubClient,
	}, nil
}

func newPubSubClient() (*pubSubClient, error) {
	envVars, err := env.ParseAs[GCPPubSubConfig]()
	if err != nil {
		return nil, err
	}
	return &pubSubClient{
		config: GCPPubSubConfig(envVars),
	}, nil
}

func newAssetClient() (*assetClient, error) {
	envVars, err := env.ParseAs[GCPAssetConfig]()
	if err != nil {
		return nil, err
	}
	return &assetClient{
		config: GCPAssetConfig(envVars),
	}, nil
}

func (p *pubSubClient) initPubSubClient(ctx context.Context) error {
	if p.c != nil {
		return nil
	}
	if err := checkPubSubConfig(p.config); err != nil {
		return err
	}
	client, err := pubsub.NewClient(ctx, p.config.ProjectID)
	if err != nil {
		return err
	}
	p.c = client
	return nil
}

func (p *pubSubClient) initPubSubSubscriber(log logger.Logger) *pubsub.Subscriber {
	log.Debug("starting to listen to Pub/Sub messages",
		"projectId", p.config.ProjectID,
		"topicName", p.config.TopicName,
		"subscriptionId", p.config.SubscriptionID,
	)
	return p.c.Subscriber(p.config.SubscriptionID)
}

func (a *assetClient) initAssetClient(ctx context.Context) error {
	if a.c != nil {
		return nil
	}
	if err := checkAssetConfig(a.config); err != nil {
		return err
	}
	client, err := asset.NewClient(ctx)
	if err != nil {
		return err
	}
	a.c = client
	return nil
}

func (p *pubSubClient) closePubSubClient(log logger.Logger) error {
	log.Debug("Initiating GCP pub/sub client shutdown")
	if p.c != nil {
		if err := p.c.Close(); err != nil {
			return err
		}
		p.c = nil
	}
	log.Debug("Completed GCP pub/sub client shutdown")
	return nil
}

func (a *assetClient) closeAssetClient(log logger.Logger) error {
	log.Debug("Initiating GCP asset client shutdown")
	if a.c != nil {
		if err := a.c.Close(); err != nil {
			return err
		}
		a.c = nil
	}
	log.Debug("Completed GCP asset client shutdown")
	return nil
}

func (g *GCPSource) Close(ctx context.Context) error {
	log := logger.FromContext(ctx).WithName(loggerName)
	log.Debug("Initiating GCP source clients shutdown")
	errorsList := make([]error, 0)
	err := g.p.closePubSubClient(log)
	if err != nil {
		errorsList = append(errorsList, err)
	}
	err = g.a.closeAssetClient(log)
	if err != nil {
		errorsList = append(errorsList, err)
	}
	if len(errorsList) > 0 {
		err := errors.Join(errorsList...)
		return fmt.Errorf("%w: %s", ErrClosingGCPSourceClient, err.Error())
	}
	log.Debug("Completed GCP source clients shutdown")
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

func (a *assetClient) getListAssetsRequest(typesToSync []string) *assetpb.ListAssetsRequest {
	return &assetpb.ListAssetsRequest{
		Parent:      "projects/" + a.config.ProjectID,
		AssetTypes:  typesToSync,
		ContentType: assetpb.ContentType_RESOURCE,
	}
}

func (g *GCPSource) StartSyncProcess(ctx context.Context, typesToSync []string, results chan<- source.SourceData) error {
	log := logger.FromContext(ctx).WithName(loggerName)
	if !g.a.startMutex.TryLock() {
		log.Debug("sync process already running")
		return nil
	}

	defer g.a.startMutex.Unlock()

	err := g.a.initAssetClient(ctx)
	if err != nil {
		err = fmt.Errorf("%w: %s", ErrClientInitialization, err.Error())
		return err
	}
	defer func() {
		if err := g.a.closeAssetClient(log); err != nil {
			err = fmt.Errorf("%w: %s", ErrClosingGCPSourceClient, err.Error())
			log.Error("error", err)
		}
	}()

	req := g.a.getListAssetsRequest(typesToSync)

	it := g.a.c.ListAssets(ctx, req)

	for {
		asset, err := it.Next()
		if err != nil {
			if errors.Is(err, iterator.Done) {
				break
			} else {
				return fmt.Errorf("%w: %s", ErrListAssetIterator, err.Error())
			}
		}
		results <- source.SourceData{
			Type:      asset.GetAssetType(),
			Operation: source.DataOperationUpsert,
			Values:    assetToMap(asset),
		}
	}
	return nil
}

func (g *GCPSource) StartEventStream(ctx context.Context, typesToStream []string, results chan<- source.SourceData) error {
	log := logger.FromContext(ctx).WithName(loggerName)
	err := g.p.initPubSubClient(ctx)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrClientInitialization, err.Error())
	}
	defer func() {
		if err = g.p.closePubSubClient(log); err != nil {
			err = fmt.Errorf("%w: %s", ErrClosingGCPSourceClient, err.Error())
			log.Error("error", err)
		}
	}()

	return g.p.gcpListener(ctx, log, typesToStream, results)
}

func (p *pubSubClient) gcpListener(ctx context.Context, log logger.Logger, typesToStream []string, results chan<- source.SourceData) error {
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
		return fmt.Errorf("%w: %s", ErrReceivePubSubMessages, err.Error())
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
