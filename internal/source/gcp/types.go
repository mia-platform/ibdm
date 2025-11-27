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
	ProjectID      string `env:"GCP_PROJECT_ID"`
	TopicName      string `env:"GCP_TOPIC_NAME"`
	SubscriptionID string `env:"GCP_SUBSCRIPTION_ID"`
}

type GCPAssetEnvironmentVariables struct {
	ProjectID string `env:"GCP_PROJECT_ID"`
}

type GCPPubSubConfig struct {
	ProjectID      string
	TopicName      string
	SubscriptionID string
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

	startMutex sync.Mutex
	c          *asset.Client
}
