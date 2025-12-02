// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package gcp

import (
	"sync"

	asset "cloud.google.com/go/asset/apiv1"
	"cloud.google.com/go/pubsub/v2"
)

type gcpPubSubConfig struct {
	ProjectID      string `env:"GOOGLE_CLOUD_PUBSUB_PROJECT"`
	SubscriptionID string `env:"GOOGLE_CLOUD_PUBSUB_SUBSCRIPTION"`
}

type gcpAssetConfig struct {
	Parent string `env:"GOOGLE_CLOUD_SYNC_PARENT"`
}

type GCPSource struct {
	p *pubSubClient
	a *assetClient
}

type pubSubClient struct {
	config gcpPubSubConfig

	c *pubsub.Client
}

type assetClient struct {
	config gcpAssetConfig

	startMutex sync.Mutex
	c          *asset.Client
}
