// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package jenkins

import (
	"context"
	"net/url"
)

const (
	wfapiDescribePath = "/wfapi/describe"

	// stageViewStatusFailed is the Stage View status of a failed stage.
	stageViewStatusFailed = "FAILED"
)

// stageViewStatuses converts the Stage View statuses to the common ones. NOT_EXECUTED covers both
// the stages skipped by a condition and those not reached after a failure.
var stageViewStatuses = map[string]string{
	"SUCCESS":              stageStatusSuccess,
	stageViewStatusFailed:  stageStatusFailure,
	"UNSTABLE":             stageStatusUnstable,
	"ABORTED":              stageStatusAborted,
	"NOT_EXECUTED":         stageStatusSkipped,
	"IN_PROGRESS":          stageStatusRunning,
	"PAUSED_PENDING_INPUT": stageStatusPaused,
}

// runDescription is the Stage View description of a build.
type runDescription struct {
	Stages []stageDescription `json:"stages"`
}

// stageDescription is the Stage View summary of a stage of a build.
type stageDescription struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	ExecNode        string     `json:"execNode"`
	Status          string     `json:"status"`
	StartTimeMillis int64      `json:"startTimeMillis"`
	DurationMillis  int64      `json:"durationMillis"`
	Error           *flowError `json:"error"`
}

// nodeDescription is the Stage View description of a flow node: a stage with its flow nodes, or
// a single step.
type nodeDescription struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	Error          *flowError `json:"error"`
	StageFlowNodes []flowNode `json:"stageFlowNodes"`
}

// flowNode is a step executed within a stage.
type flowNode struct {
	ID    string     `json:"id"`
	Name  string     `json:"name"`
	Error *flowError `json:"error"`
}

// flowError is the error recorded on a failed stage or flow node.
type flowError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

// nodeError is an error read from Stage View. stepID and stepName are set when the error was
// recorded on a step within the described node.
type nodeError struct {
	message  string
	errType  string
	stepID   string
	stepName string
}

// stageViewStages returns the stages of a build read from Pipeline: Stage View, which exposes a
// flat list without hierarchy.
func (w *walker) stageViewStages(ctx context.Context, buildPath, buildURL string) ([]*stageEntry, error) {
	var run runDescription
	if _, err := w.client.getJSON(ctx, buildPath+wfapiDescribePath, "", &run); err != nil {
		return nil, err
	}

	entries := make([]*stageEntry, 0, len(run.Stages))
	for _, stage := range run.Stages {
		entry := &stageEntry{
			id:              stage.ID,
			name:            stage.Name,
			status:          stageViewStatus(stage.Status),
			startTimeMillis: stage.StartTimeMillis,
			durationMillis:  stage.DurationMillis,
			agent:           stage.ExecNode,
			children:        []string{},
		}
		if stage.Status == stageViewStatusFailed {
			entry.failure = w.stageViewFailure(ctx, buildPath, buildURL, stage)
		}
		entries = append(entries, entry)
	}

	return entries, nil
}

// stageViewFailure returns the failure details of a failed stage, taken from the first step of
// the stage that recorded an error, or from the error recorded on the stage itself.
func (w *walker) stageViewFailure(ctx context.Context, buildPath, buildURL string, stage stageDescription) map[string]any {
	failure := newFailure(buildURL)

	if described, ok := w.stageViewNodeError(ctx, buildPath, stage.ID); ok {
		setFailureError(failure, described.message, described.errType)
		if described.stepID != "" {
			setFailedStep(failure, buildURL, described.stepID, described.stepName)
		}
		return failure
	}

	if stage.Error != nil {
		setFailureError(failure, stage.Error.Message, stage.Error.Type)
	}
	return failure
}

// stageViewNodeError reads the error of a flow node from Stage View. For a stage, the error of
// its first failed step is preferred over the error of the stage itself. The boolean is false
// when the node could not be read or recorded no error. While Stage View is still being probed,
// the outcome of the call decides whether it is available.
func (w *walker) stageViewNodeError(ctx context.Context, buildPath, nodeID string) (nodeError, bool) {
	var described nodeDescription
	nodePath := buildPath + "/execution/node/" + url.PathEscape(nodeID) + wfapiDescribePath
	_, err := w.client.getJSON(ctx, nodePath, "", &described)
	if w.stageAPIs.stageView == apiUnknown {
		if err == nil {
			w.stageAPIs.stageView = apiAvailable
		} else if isNotFound(err) {
			w.stageAPIs.stageView = apiUnavailable
		}
	}
	if err != nil {
		if ctx.Err() == nil {
			w.log.Debug("failed to read node error from stage view", "node", nodePath, "error", err.Error())
		}
		return nodeError{}, false
	}

	for _, node := range described.StageFlowNodes {
		if node.Error != nil {
			return nodeError{message: node.Error.Message, errType: node.Error.Type, stepID: node.ID, stepName: node.Name}, true
		}
	}

	if described.Error != nil {
		return nodeError{message: described.Error.Message, errType: described.Error.Type}, true
	}

	return nodeError{}, false
}

// stageViewStatus converts a Stage View status to the common one.
func stageViewStatus(status string) string {
	if converted, ok := stageViewStatuses[status]; ok {
		return converted
	}
	return stageStatusUnknown
}
