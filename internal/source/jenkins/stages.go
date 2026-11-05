// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package jenkins

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	graphViewPluginName = "pipeline-graph-view"
	stageViewPluginName = "pipeline-stage-view"

	// pluginStateTree selects only what is needed to know whether a plugin is usable.
	pluginStateTree = "plugins[shortName,active,enabled]"

	// Common stage statuses, shared by both plugins.
	stageStatusSuccess  = "success"
	stageStatusFailure  = "failure"
	stageStatusUnstable = "unstable"
	stageStatusAborted  = "aborted"
	stageStatusSkipped  = "skipped"
	stageStatusRunning  = "running"
	stageStatusPaused   = "paused"
	stageStatusUnknown  = "unknown"

	// maxErrorMessageLength bounds, in characters, the failure message kept for a stage.
	maxErrorMessageLength = 2048
	truncationSuffix      = "..."
)

// apiState records whether a stage plugin answered on this controller.
type apiState int

const (
	apiUnknown apiState = iota
	apiAvailable
	apiUnavailable
)

// stageAPIs tracks, for the duration of a sync, which stage plugins are usable. The plugin list
// is read once; when it is not readable, each plugin is probed on the first build that needs it
// and the outcome is reused for every other workflow.
type stageAPIs struct {
	detected  bool
	warned    bool
	graphView apiState
	stageView apiState
}

// stageEntry is a stage of a build in the structure shared by both plugins.
type stageEntry struct {
	id              string
	name            string
	status          string
	startTimeMillis int64
	durationMillis  int64
	agent           string
	parallel        bool
	synthetic       bool
	children        []string
	failure         map[string]any
}

// values returns the stage as exposed to the mappings.
func (e *stageEntry) values() map[string]any {
	values := map[string]any{
		"id":              e.id,
		keyName:           e.name,
		"status":          e.status,
		"startTimeMillis": e.startTimeMillis,
		"durationMillis":  e.durationMillis,
		"agent":           e.agent,
		"parallel":        e.parallel,
		"synthetic":       e.synthetic,
		"children":        e.children,
	}
	if e.failure != nil {
		values["failure"] = e.failure
	}
	return values
}

// fetchStages returns the stages of a build as a flat list in execution order, with the plugin
// they were read from. Pipeline Graph View is preferred because it exposes the hierarchy of the
// stages; Pipeline: Stage View is used when Graph View is not available. The boolean is false
// when neither plugin could provide the stages: the workflow is then emitted without them.
func (w *walker) fetchStages(ctx context.Context, segments []string, build *buildInfo) ([]map[string]any, string, bool) {
	w.detectStageAPIs(ctx)
	buildPath := jobPath(segments) + "/" + strconv.FormatInt(build.Number, 10)

	if w.stageAPIs.graphView != apiUnavailable {
		entries, err := w.graphViewStages(ctx, buildPath, build.URL)
		if ok := w.recordOutcome(ctx, &w.stageAPIs.graphView, graphViewPluginName, build.URL, err); ok {
			return entryValues(entries), graphViewPluginName, true
		}
		if ctx.Err() != nil {
			return nil, "", false
		}
	}

	if w.stageAPIs.stageView != apiUnavailable {
		entries, err := w.stageViewStages(ctx, buildPath, build.URL)
		if ok := w.recordOutcome(ctx, &w.stageAPIs.stageView, stageViewPluginName, build.URL, err); ok {
			return entryValues(entries), stageViewPluginName, true
		}
		if ctx.Err() != nil {
			return nil, "", false
		}
	}

	if w.stageAPIs.graphView == apiUnavailable && w.stageAPIs.stageView == apiUnavailable && !w.stageAPIs.warned {
		w.stageAPIs.warned = true
		w.log.Warn("neither pipeline graph view nor pipeline stage view is available, emitting workflows without stages")
	}
	return nil, "", false
}

// recordOutcome updates the state of a stage plugin after a call and reports whether the call
// succeeded. A 404 marks the plugin as unavailable for the rest of the sync; other failures are
// logged and leave the state unchanged.
func (w *walker) recordOutcome(ctx context.Context, state *apiState, plugin, buildURL string, err error) bool {
	switch {
	case err == nil:
		*state = apiAvailable
		return true
	case ctx.Err() != nil:
		return false
	case isNotFound(err):
		w.log.Debug("stage plugin not available", "plugin", plugin)
		*state = apiUnavailable
		return false
	default:
		w.log.Warn("failed to read stages", "plugin", plugin, "build", buildURL, "error", err.Error())
		return false
	}
}

// detectStageAPIs reads, once per sync, which stage plugins are installed and enabled. When the
// plugin list cannot be read, usually for lack of the Overall/Administer or Overall/SystemRead
// permission, both plugins are left unknown and probed on the first build.
func (w *walker) detectStageAPIs(ctx context.Context) {
	if w.stageAPIs.detected {
		return
	}
	w.stageAPIs.detected = true

	var list pluginList
	if _, err := w.client.getJSON(ctx, pluginManagerPath+apiJSONPath, pluginStateTree, &list); err != nil {
		if ctx.Err() == nil {
			w.log.Debug("plugin list not readable, probing the stage plugins", "error", err.Error())
		}
		return
	}

	w.stageAPIs.graphView = apiUnavailable
	w.stageAPIs.stageView = apiUnavailable
	for _, plugin := range list.Plugins {
		if !plugin.Active || !plugin.Enabled {
			continue
		}
		switch plugin.ShortName {
		case graphViewPluginName:
			w.stageAPIs.graphView = apiAvailable
		case stageViewPluginName:
			w.stageAPIs.stageView = apiAvailable
		}
	}
}

// entryValues returns the values of every stage entry.
func entryValues(entries []*stageEntry) []map[string]any {
	out := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.values())
	}
	return out
}

// newFailure returns the failure details every failed stage carries: the console of the build.
func newFailure(buildURL string) map[string]any {
	return map[string]any{"consoleUrl": withTrailingSlash(buildURL) + "console"}
}

// setFailedStep records on failure the step that failed, with the link to its log.
func setFailedStep(failure map[string]any, buildURL, stepID, stepName string) {
	failure["nodeName"] = stepName
	failure["logUrl"] = withTrailingSlash(buildURL) + "execution/node/" + url.PathEscape(stepID) + "/log/"
}

// setFailureError records on failure the message and the type of the error, when known.
func setFailureError(failure map[string]any, message, errType string) {
	if message != "" {
		failure["message"] = truncate(message, maxErrorMessageLength)
	}
	if errType != "" {
		failure["type"] = errType
	}
}

// truncate shortens s to at most limit characters, marking the cut with truncationSuffix.
func truncate(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}

	runes := []rune(s)
	return string(runes[:limit]) + truncationSuffix
}

// withTrailingSlash returns rawURL ending with a slash, as the URLs returned by Jenkins do.
func withTrailingSlash(rawURL string) string {
	if strings.HasSuffix(rawURL, "/") {
		return rawURL
	}
	return rawURL + "/"
}
