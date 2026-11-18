// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package server

import (
	"encoding/json"
	"net/http"

	"github.com/mia-platform/ibdm/internal/logger"
)

const (
	headerContentType = "Content-Type"
	contentTypeJSON   = "application/json"
)

// errorResponse is the body of every error answered by the server.
type errorResponse struct {
	Error      string `json:"error"`
	Message    string `json:"message"`
	StatusCode int    `json:"statusCode"`
}

// writeJSON sends v encoded as JSON with the given status code. An encoding failure is logged
// and answered with an empty 500, since nothing has been sent yet.
func writeJSON(w http.ResponseWriter, log logger.Logger, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		log.Error("encoding response body", "error", err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set(headerContentType, contentTypeJSON)
	w.WriteHeader(status)
	if _, err := w.Write(body); err != nil {
		log.Debug("writing response body", "error", err.Error())
	}
}

// writeError sends an errorResponse with the given status code and message.
func writeError(w http.ResponseWriter, log logger.Logger, status int, message string) {
	writeJSON(w, log, status, errorResponse{
		Error:      http.StatusText(status),
		Message:    message,
		StatusCode: status,
	})
}
