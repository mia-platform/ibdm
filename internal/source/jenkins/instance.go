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
	instanceTree  = "mode,nodeDescription,useSecurity,quietingDown"
)

// instanceInfo holds the general settings of the controller.
type instanceInfo struct {
	Mode            string `json:"mode"`
	NodeDescription string `json:"nodeDescription"`
	UseSecurity     bool   `json:"useSecurity"`
	QuietingDown    bool   `json:"quietingDown"`
}

// syncInstance emits the controller with its version and general settings.
func (s *Source) syncInstance(ctx context.Context, results chan<- source.Data) error {
	var info instanceInfo
	headers, err := s.client.getJSON(ctx, apiJSONPath, instanceTree, &info)
	if err != nil {
		return err
	}

	values := map[string]any{
		"instance": map[string]any{
			keyURL:            s.client.baseURL + "/",
			"version":         headers.Get(versionHeader),
			"mode":            info.Mode,
			"nodeDescription": info.NodeDescription,
			"useSecurity":     info.UseSecurity,
			"quietingDown":    info.QuietingDown,
		},
	}

	return send(ctx, results, instanceType, values)
}
