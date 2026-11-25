// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package server

import (
	"net/http"

	"github.com/mia-platform/ibdm/internal/logger"
)

const (
	// statusRoutesPrefix is the path prefix shared by the status routes.
	statusRoutesPrefix = "/-/"
)

// statusResponse type.
type statusResponse struct {
	Status  string `json:"status"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

// statusRoutes registers the health and readiness routes on mux.
func statusRoutes(mux *http.ServeMux, serviceName, serviceVersion string) {
	status := statusResponse{
		Status:  "OK",
		Name:    serviceName,
		Version: serviceVersion,
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, logger.FromContext(r.Context()), http.StatusOK, status)
	})

	for _, route := range []string{"healthz", "ready", "check-up"} {
		mux.Handle(http.MethodGet+" "+statusRoutesPrefix+route, handler)
	}
}
