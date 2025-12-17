// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package gcp

import (
	"sync"
	"sync/atomic"

	asset "cloud.google.com/go/asset/apiv1"
	"cloud.google.com/go/pubsub/v2"
)

// pubSubConfig holds the environment-driven Pub/Sub settings.
type pubSubConfig struct {
	ProjectID      string `env:"GOOGLE_CLOUD_PUBSUB_PROJECT"`
	SubscriptionID string `env:"GOOGLE_CLOUD_PUBSUB_SUBSCRIPTION"`
}

// assetConfig holds the environment-driven Cloud Asset settings.
type assetConfig struct {
	Parent string `env:"GOOGLE_CLOUD_SYNC_PARENT"`
}

// Source wires Cloud Asset and Pub/Sub clients to satisfy source interfaces.
type Source struct {
	p *pubSubClient
	a *assetClient
}

// pubSubClient lazily initializes a Pub/Sub client.
type pubSubClient struct {
	config pubSubConfig

	c atomic.Pointer[pubsub.Client]
}

// assetClient lazily initializes a Cloud Asset client.
type assetClient struct {
	config assetConfig

	startMutex sync.Mutex
	c          atomic.Pointer[asset.Client]
}
