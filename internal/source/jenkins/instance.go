// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package jenkins

import (
	"context"

	"github.com/mia-platform/ibdm/internal/source"
)

const (
	// versionHeader is the response header carrying the version of the controller.
	versionHeader = "X-Jenkins"
	instanceTree  = "description,mode"
)

// instanceInfo holds the general settings of the controller.
type instanceInfo struct {
	// Description is the system message of the controller, null when it is not set.
	Description string `json:"description"`
	// Mode is the usage mode of the built-in node: NORMAL or EXCLUSIVE.
	Mode string `json:"mode"`
}

// syncInstance emits the controller with its version, its system message and the usage mode of
// its built-in node.
func (s *Source) syncInstance(ctx context.Context, results chan<- source.Data) error {
	var info instanceInfo
	headers, err := s.client.getJSON(ctx, apiJSONPath, instanceTree, &info)
	if err != nil {
		return err
	}

	values := map[string]any{
		"instance": map[string]any{
			keyURL:         s.client.baseURL + "/",
			"version":      headers.Get(versionHeader),
			keyDescription: info.Description,
			"mode":         info.Mode,
		},
	}

	return send(ctx, results, instanceType, values)
}
