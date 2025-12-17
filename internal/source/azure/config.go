// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package azure

// config holds all the configuration needed to connect to Azure.
type config struct{}

// validateForSync checks if the configuration is valid for sync operations.
func (c config) validateForSync() error {
	return nil
}

// validateForEventStream checks if the configuration is valid for event stream operations.
func (c config) validateForEventStream() error {
	return nil
}
