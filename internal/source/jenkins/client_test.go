// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package jenkins

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetJSON(t *testing.T) {
	t.Parallel()

	t.Run("sends credentials and tree, returns headers", func(t *testing.T) {
		t.Parallel()

		var gotTree, gotAccept string
		src := newTestSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, password, ok := r.BasicAuth()
			assert.True(t, ok)
			assert.Equal(t, testUsername, user)
			assert.Equal(t, testAPIToken, password)
			assert.Equal(t, http.MethodGet, r.Method)
			gotTree = r.URL.Query().Get(treeQueryParam)
			gotAccept = r.Header.Get("Accept")
			w.Header().Set(versionHeader, "2.555.1")
			_, _ = w.Write([]byte(`{"mode":"NORMAL"}`))
		}))

		var info instanceInfo
		headers, err := src.client.getJSON(t.Context(), apiJSONPath, instanceTree, &info)
		require.NoError(t, err)
		assert.Equal(t, "NORMAL", info.Mode)
		assert.Equal(t, "2.555.1", headers.Get(versionHeader))
		assert.Equal(t, instanceTree, gotTree)
		assert.Equal(t, "application/json", gotAccept)
	})

	t.Run("omits empty tree", func(t *testing.T) {
		t.Parallel()

		var rawQuery string
		src := newTestSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rawQuery = r.URL.RawQuery
			_, _ = w.Write([]byte(`{}`))
		}))

		var out map[string]any
		_, err := src.client.getJSON(t.Context(), "/job/my-job/1/wfapi/describe", "", &out)
		require.NoError(t, err)
		assert.Empty(t, rawQuery)
	})

	t.Run("non 2xx returns a bounded status error", func(t *testing.T) {
		t.Parallel()

		src := newTestSource(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(strings.Repeat("x", 4*maxErrorBodySize)))
		}))

		var out map[string]any
		_, err := src.client.getJSON(t.Context(), apiJSONPath, "", &out)
		var statusErr *statusError
		require.ErrorAs(t, err, &statusErr)
		assert.Equal(t, http.StatusForbidden, statusErr.StatusCode)
		assert.Len(t, statusErr.Body, maxErrorBodySize)
		assert.NotContains(t, err.Error(), testAPIToken)
	})

	t.Run("invalid JSON", func(t *testing.T) {
		t.Parallel()

		src := newTestSource(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{not json`))
		}))

		var out map[string]any
		_, err := src.client.getJSON(t.Context(), apiJSONPath, "", &out)
		assert.ErrorContains(t, err, "failed to decode")
	})

	t.Run("canceled context", func(t *testing.T) {
		t.Parallel()

		src := newTestSource(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{}`))
		}))

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		var out map[string]any
		_, err := src.client.getJSON(ctx, apiJSONPath, "", &out)
		require.ErrorIs(t, err, context.Canceled)
	})

	t.Run("invalid base URL", func(t *testing.T) {
		t.Parallel()

		c := &client{baseURL: "http://ci.example.com\x7f", httpClient: http.DefaultClient}
		var out map[string]any
		_, err := c.getJSON(t.Context(), apiJSONPath, "", &out)
		assert.ErrorContains(t, err, "failed to create request")
	})
}

func TestJobPath(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		segments []string
		expected string
	}{
		"root":                {segments: nil, expected: ""},
		"nested":              {segments: []string{"my-team", "my-app"}, expected: "/job/my-team/job/my-app"},
		"encoded branch name": {segments: []string{"my-app", "feature%2Flogin"}, expected: "/job/my-app/job/feature%252Flogin"},
		"reserved characters": {segments: []string{"my team", "a/b?c#d"}, expected: "/job/my%20team/job/a%2Fb%3Fc%23d"},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, jobPath(tc.segments))
		})
	}
}

func TestAppendSegmentDoesNotShareBackingArray(t *testing.T) {
	t.Parallel()

	base := make([]string, 1, 4)
	base[0] = "my-team"

	first := appendSegment(base, "my-app")
	second := appendSegment(base, "my-service")

	assert.Equal(t, []string{"my-team", "my-app"}, first)
	assert.Equal(t, []string{"my-team", "my-service"}, second)
}

func TestStatusErrorMessage(t *testing.T) {
	t.Parallel()

	err := &statusError{StatusCode: http.StatusNotFound, Body: "not found"}
	assert.Equal(t, "jenkins API returned status 404: not found", err.Error())
}
