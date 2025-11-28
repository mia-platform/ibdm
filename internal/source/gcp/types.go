// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package gcp

import (
	"context"
	"sync"

	asset "cloud.google.com/go/asset/apiv1"
	"cloud.google.com/go/pubsub/v2"
)

type ListenerFunc func(ctx context.Context, data []byte) error

type GCPPubSubEnvironmentVariables struct {
	ProjectID      string `env:"GOOGLE_CLOUD_PUBSUB_PROJECT"`
	TopicName      string `env:"GOOGLE_CLOUD_PUBSUB_TOPIC"`
	SubscriptionID string `env:"GOOGLE_CLOUD_PUBSUB_SUBSCRIPTION"`
}

type GCPAssetEnvironmentVariables struct {
	ProjectID string `env:"GOOGLE_CLOUD_ASSET_PROJECT"`
}

type GCPPubSubConfig struct {
	ProjectID      string
	TopicName      string
	SubscriptionID string
}

type GCPAssetConfig struct {
	ProjectID string
}

type GCPSource struct {
	p *pubSubClient
	a *assetClient
}

type pubSubClient struct {
	config GCPPubSubConfig

	c *pubsub.Client
}

type assetClient struct {
	config GCPAssetConfig

	startMutex sync.Mutex
	c          *asset.Client
}
