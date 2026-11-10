// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package jenkins

import (
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWalkStopsAtMaxFolderDepth(t *testing.T) {
	t.Parallel()

	// Every container holds one nested folder, forever.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		depth := strings.Count(r.URL.EscapedPath(), "/job/")
		name := fmt.Sprintf("level-%d", depth)
		_, _ = fmt.Fprintf(w, `{"jobs": [{"_class": %q, "name": %q, "fullName": %q, "url": "https://ci.example.com/", "jobs": []}]}`,
			folderClass, name, name)
	})

	counter := &countingHandler{next: handler}
	src := newTestSource(t, counter)

	inv, err := src.walkJobs(t.Context(), walkOptions{}, nil)
	require.NoError(t, err)
	assert.Len(t, inv.containers, maxFolderDepth)
	assert.Equal(t, int64(maxFolderDepth), counter.calls.Load())
}

// countingHandler counts the requests it forwards to next.
type countingHandler struct {
	next  http.Handler
	calls atomic.Int64
}

func (h *countingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.calls.Add(1)
	h.next.ServeHTTP(w, r)
}

func TestWalkEntersUnknownContainers(t *testing.T) {
	t.Parallel()

	routes := map[string]fakeResponse{
		rootListingPath:        {body: `{"jobs": [{"_class": "com.example.CustomFolder", "name": "custom", "fullName": "custom", "url": "https://ci.example.com/job/custom/", "jobs": [{"name": "inner"}]}]}`},
		"/job/custom/api/json": {body: `{"jobs": [{"_class": "` + workflowJobClass + `", "name": "inner", "fullName": "custom/inner", "url": "https://ci.example.com/job/custom/job/inner/", "lastCompletedBuild": null}]}`},
	}

	data, _, err := runSync(t, routes, folderType, workflowType)
	require.NoError(t, err)
	require.Len(t, data, 1)
	assert.Equal(t, workflowType, data[0].Type)
	parent := data[0].Values["parent"].(map[string]any)
	assert.Equal(t, parentKindInstance, parent[keyKind])
}

func TestObjectURLAndHasAction(t *testing.T) {
	t.Parallel()

	actions := []jobAction{
		{},
		{Class: objectMetadataActionClass},
		{Class: objectMetadataActionClass, ObjectURL: "https://git.example.com/my-org/my-app"},
		{Class: primaryInstanceActionClass},
	}

	assert.Equal(t, "https://git.example.com/my-org/my-app", objectURL(actions))
	assert.Empty(t, objectURL(nil))
	assert.True(t, hasAction(actions, primaryInstanceActionClass))
	assert.False(t, hasAction(actions[:2], primaryInstanceActionClass))
}
