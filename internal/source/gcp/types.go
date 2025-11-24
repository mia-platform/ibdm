// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package gcp

import (
	"context"

	asset "cloud.google.com/go/asset/apiv1"
	"cloud.google.com/go/pubsub/v2"

	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/mia-platform/ibdm/internal/source"
)

type GCPAssetInterface interface {
	newGCPAssetInstance(ctx context.Context) (gcpAssetInstance, error)
	initAssetClient(ctx context.Context) error
	closeAssetClient(ctx context.Context) error
	StartSyncProcess(ctx context.Context, typesToSync []string, results chan<- source.SourceData) (err error)
}

type ListenerFunc func(ctx context.Context, data []byte) error

type GCPPubSubEnvironmentVariables struct {
	ProjectID      string `env:"GCP_PROJECT_ID"`
	TopicName      string `env:"GCP_TOPIC_NAME"`
	SubscriptionID string `env:"GCP_SUBSCRIPTION_ID"`
}

type GCPAssetEnvironmentVariables struct {
	ProjectID string `env:"GCP_PROJECT_ID"`
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
	p   *gcpPubSubInstance
	a   *gcpAssetInstance
	log logger.Logger
}

type gcpPubSubInstance struct {
	config GCPPubSubConfig

	c *pubsub.Client
	s *pubsub.Subscriber
}

type gcpAssetInstance struct {
	config GCPAssetConfig

	c *asset.Client
}
