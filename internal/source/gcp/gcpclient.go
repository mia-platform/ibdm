// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package gcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	asset "cloud.google.com/go/asset/apiv1"
	"cloud.google.com/go/asset/apiv1/assetpb"
	"cloud.google.com/go/pubsub/v2"
	"github.com/caarlos0/env/v11"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/mia-platform/ibdm/internal/source"
)

const (
	loggerName = "ibdm:source:gcp"
)

var (
	ErrMissingEnvVariable = errors.New("missing environment variable")
	ErrInvalidEnvVariable = errors.New("invalid environment value")
	ErrGCPSource          = errors.New("gcp source")

	syncParentRegex = regexp.MustCompile(`^(projects|organizations|folders)\/.*`)
)

func checkPubSubConfig(cfg gcpPubSubConfig) error {
	missingEnvs := make([]string, 0)
	if cfg.ProjectID == "" {
		missingEnvs = append(missingEnvs, "GOOGLE_CLOUD_PUBSUB_PROJECT")
	}
	if cfg.SubscriptionID == "" {
		missingEnvs = append(missingEnvs, "GOOGLE_CLOUD_PUBSUB_SUBSCRIPTION")
	}

	if len(missingEnvs) > 0 {
		return fmt.Errorf("%w: %s", ErrMissingEnvVariable, strings.Join(missingEnvs, ", "))
	}

	return nil
}

func checkAssetConfig(cfg gcpAssetConfig) error {
	if cfg.Parent == "" {
		return fmt.Errorf("%w: %s", ErrMissingEnvVariable, "GOOGLE_CLOUD_SYNC_PARENT")
	}

	if !syncParentRegex.MatchString(cfg.Parent) {
		return fmt.Errorf("%w: %s", ErrInvalidEnvVariable, "GOOGLE_CLOUD_SYNC_PARENT must be one of 'organizations/[organization-number]', 'projects/[project-id]', 'projects/[project-number]', or 'folders/[folder-number]'")
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
		return nil, handleError(errors.Join(errorsList...))
	}

	return &GCPSource{
		a: assetClient,
		p: pubSubClient,
	}, nil
}

func newPubSubClient() (*pubSubClient, error) {
	pubSubConfig, err := env.ParseAs[gcpPubSubConfig]()
	if err != nil {
		return nil, err
	}
	return &pubSubClient{
		config: pubSubConfig,
	}, nil
}

func newAssetClient() (*assetClient, error) {
	assetConfig, err := env.ParseAs[gcpAssetConfig]()
	if err != nil {
		return nil, err
	}
	return &assetClient{
		config: assetConfig,
	}, nil
}

func (p *pubSubClient) initPubSubClient(ctx context.Context) (*pubsub.Client, error) {
	client := p.c.Load()
	if client != nil {
		return client, nil
	}

	if err := checkPubSubConfig(p.config); err != nil {
		return nil, err
	}

	client, err := pubsub.NewClient(ctx, p.config.ProjectID)
	if err != nil {
		return nil, err
	}

	p.c.Store(client)
	return client, nil
}

func (a *assetClient) initAssetClient(ctx context.Context) (*asset.Client, error) {
	client := a.c.Load()
	if client != nil {
		return client, nil
	}

	if err := checkAssetConfig(a.config); err != nil {
		return nil, err
	}
	client, err := asset.NewClient(ctx)
	if err != nil {
		return nil, err
	}

	a.c.Store(client)
	return client, nil
}

func (p *pubSubClient) closePubSubClient(log logger.Logger) error {
	log.Debug("Initiating GCP pub/sub client shutdown")
	client := p.c.Swap(nil)
	if client != nil {
		if err := handleError(client.Close()); err != nil {
			return err
		}
	}

	log.Debug("Completed GCP pub/sub client shutdown")
	return nil
}

func (a *assetClient) closeAssetClient(log logger.Logger) error {
	log.Debug("Initiating GCP asset client shutdown")
	client := a.c.Swap(nil)
	if client != nil {
		if err := client.Close(); err != nil {
			return err
		}
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
		return handleError(errors.Join(errorsList...))
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

func (a *assetClient) getListAssetsRequest(typesToSync []string) *assetpb.ListAssetsRequest {
	return &assetpb.ListAssetsRequest{
		Parent:      a.config.Parent,
		AssetTypes:  typesToSync,
		ContentType: assetpb.ContentType_RESOURCE,
	}
}

func (g *GCPSource) StartSyncProcess(ctx context.Context, typesToSync []string, results chan<- source.Data) error {
	log := logger.FromContext(ctx).WithName(loggerName)
	if !g.a.startMutex.TryLock() {
		log.Debug("sync process already running")
		return nil
	}

	defer g.a.startMutex.Unlock()

	client, err := g.a.initAssetClient(ctx)
	if err := handleError(err); err != nil {
		return err
	}

	defer func() {
		if err := handleError(g.a.closeAssetClient(log)); err != nil {
			log.Error("error", err)
		}
	}()

	req := g.a.getListAssetsRequest(typesToSync)
	it := client.ListAssets(ctx, req)

	for {
		asset, err := it.Next()
		if err != nil {
			if errors.Is(err, iterator.Done) {
				break
			} else {
				return handleError(err)
			}
		}

		results <- source.Data{
			Type:      asset.GetAssetType(),
			Operation: source.DataOperationUpsert,
			Values:    assetToMap(asset),
		}
	}
	return nil
}

func (g *GCPSource) StartEventStream(ctx context.Context, typesToStream []string, results chan<- source.Data) error {
	log := logger.FromContext(ctx).WithName(loggerName)
	client, err := g.p.initPubSubClient(ctx)
	if err := handleError(err); err != nil {
		return err
	}

	defer func() {
		if err := handleError(g.p.closePubSubClient(log)); err != nil {
			log.Error("error", err)
		}
	}()

	log.Debug("starting pubsub subscriber",
		"projectId", g.p.config.ProjectID,
		"subscriptionId", g.p.config.SubscriptionID,
	)

	subscriber := client.Subscriber(g.p.config.SubscriptionID)
	err = subscriber.Receive(ctx, func(_ context.Context, msg *pubsub.Message) {
		event, err := gcpListenerHandler(msg.Data)
		if err != nil {
			// TODO: manage to create the subscription if does not exist
			log.Error("failed to handle Pub/Sub message", "messageId", msg.ID, "error", err)
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
			valuesMap = event.GetPriorAsset()
		} else {
			valuesMap = event.GetAsset()
		}

		if valuesMap == nil {
			log.Error("failed to convert asset to map",
				"messageId", msg.ID,
				"eventType", event.GetAssetType(),
			)
			msg.Nack()
			return
		}

		results <- source.Data{
			Type:      event.GetAssetType(),
			Operation: event.Operation(),
			Values:    valuesMap,
		}
		msg.Ack()
	})

	return handleError(err)
}

func gcpListenerHandler(data []byte) (*GCPEvent, error) {
	var event *GCPEvent
	if err := json.Unmarshal(data, &event); err != nil {
		return nil, err
	}

	return event, nil
}

func handleError(err error) error {
	if err == nil {
		return nil
	}

	switch u := errors.Unwrap(err); u != nil {
	case errors.Is(u, ErrMissingEnvVariable):
	case errors.Is(u, ErrInvalidEnvVariable):
	default:
		err = u
	}

	if statusErr, ok := status.FromError(err); ok {
		err = errors.New(statusErr.Message())
	}

	return fmt.Errorf("%w: %w", ErrGCPSource, err)
}
