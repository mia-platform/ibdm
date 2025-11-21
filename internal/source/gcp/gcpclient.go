// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

/*
ogni funzione si gestisce la sua parte di variabili di ambiente così si evita di centralizzare in un punto unico il loro parse

gestione tramite sync mutex per evitare race condition di lanci successivi, ogni source se lo gestisce in loco (nessuna gestione centralizzata uguale per tutti)
mutex.TryLock() per evitare deadlock in caso di chiamate ricorsive con return nel caso in cui non possa acquisire il lock

ricordarsi di usare il loggerName per inizializzare il logger in ogni file usando .withName(loggerName)
*/

package gcp

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/caarlos0/env/v11"

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

type GCPAssetInterface interface {
	newGCPAssetInstance(ctx context.Context) (gcpAssetInstance, error)
	initAssetClient(ctx context.Context) error
	closeAssetClient(ctx context.Context) error
	StartSyncProcess(ctx context.Context, typesToSync []string, results chan<- source.SourceData) (err error)
}

type GCPPubSubEnvironmentVariables struct {
	ProjectID      string `json:"projectId"`
	TopicName      string `json:"topicName"`
	SubscriptionID string `json:"subscriptionId"`
}

type GCPAssetEnvironmentVariables struct {
	ProjectID string `json:"projectId"`
}

type GCPPubSubConfig struct {
	ProjectID       string
	TopicName       string
	SubscriptionID  string
	CredentialsJSON string
}

type GCPAssetConfig struct {
	ProjectID string
}

type GCPInstance struct {
	p *gcpPubSubInstance
	a *gcpAssetInstance
}

type gcpPubSubInstance struct {
	config GCPPubSubConfig

	c *pubsub.Client
}

type gcpAssetInstance struct {
	config GCPAssetConfig

	c *asset.Client
}

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
	assetInstance, err := newGCPAssetInstance(ctx)
	if err != nil {
		return nil, err
	}
	pubSubInstance, err := newGCPPubSubInstance(ctx)
	if err != nil {
		return nil, err
	}
	return &GCPInstance{
		a: assetInstance,
		p: pubSubInstance,
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

func (p *gcpPubSubInstance) initPubSubClient(ctx context.Context) error {
	client, err := pubsub.NewClient(ctx, p.config.ProjectID)
	if err != nil {
		return err
	}
	p.c = client
	return nil
}

func (p *gcpAssetInstance) initAssetClient(ctx context.Context) error {
	client, err := asset.NewClient(ctx)
	if err != nil {
		return err
	}
	p.c = client
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
		g.a.initAssetClient(ctx)
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
			Type:      "gcpAsset",
			Operation: source.DataOperationUpsert,
			Values:    assetToMap(asset),
		}
	}

	return nil
}

// func (a *gcpAssetInstance) ListAssets(ctx context.Context, assetTypes []string) ([]*assetpb.Asset, error) {
// 	if a.c == nil {
// 		a.initAssetClient(ctx)
// 	}
// 	defer func() {
// 		a.closeAssetClient(ctx)
// 	}()
// 	req := &assetpb.ListAssetsRequest{
// 		Parent:      "projects/" + a.config.ProjectID,
// 		AssetTypes:  assetTypes,
// 		ContentType: assetpb.ContentType_RESOURCE,
// 	}

// 	it := a.c.ListAssets(ctx, req)

// 	assets := make([]*assetpb.Asset, 0)

// 	for {
// 		response, err := it.Next()
// 		if errors.Is(err, iterator.Done) {
// 			break
// 		}
// 		assets = append(assets, response)
// 	}
// 	return assets, nil
// }
