// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package jenkins

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

const (
	graphViewTreePath          = "/stages/tree"
	graphViewAllStepsPath      = "/stages/allSteps"
	graphViewExceptionTextPath = "/stages/exceptionText"
	graphViewNodeIDParam       = "nodeId"
	graphViewStatusOK          = "ok"
	graphViewParallelType      = "PARALLEL"
	graphViewStepType          = "STEP"

	// maxExceptionTextSize bounds the exception text read from Pipeline Graph View: only its first
	// lines are kept, and the message is truncated to maxErrorMessageLength anyway.
	maxExceptionTextSize = 64 * 1024
)

// exceptionHeadPattern matches the head of a Java exception, "fully.qualified.ClassName" followed
// either by the end of the text or by a colon and the message.
var exceptionHeadPattern = regexp.MustCompile(`(?s)^((?:[A-Za-z_$][\w$]*\.)+[A-Z][\w$]*)(?::\s*(.*))?$`)

// graphTreeResponse is the Pipeline Graph View tree of the stages of a build.
type graphTreeResponse struct {
	Status string `json:"status"`
	Data   struct {
		Stages []graphStage `json:"stages"`
	} `json:"data"`
}

// graphStage is a stage of the Pipeline Graph View tree. Fields without a value are omitted
// from the response: a missing duration or agent decodes to its zero value.
type graphStage struct {
	ID                  string       `json:"id"`
	Name                string       `json:"name"`
	State               string       `json:"state"`
	Type                string       `json:"type"`
	StartTimeMillis     int64        `json:"startTimeMillis"`
	TotalDurationMillis int64        `json:"totalDurationMillis"`
	Agent               string       `json:"agent"`
	Synthetic           bool         `json:"synthetic"`
	Children            []graphStage `json:"children"`
	// NextSibling is the next stage of a sequence, which may also be listed among the children of
	// the same parent.
	NextSibling *graphStage `json:"nextSibling"`
}

// graphStepsResponse is the Pipeline Graph View list of every step of a build.
type graphStepsResponse struct {
	Status string `json:"status"`
	Data   struct {
		Steps []graphStep `json:"steps"`
	} `json:"data"`
}

// graphStep is a step of a build with the stage it belongs to. Title is the descriptive name of
// the step, empty for some steps such as Shell Script; Name holds the arguments of the step, its
// label, or its name when it has no arguments. Type is STEP, or UNHANDLED_EXCEPTION for an error
// thrown outside any step.
type graphStep struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Title   string `json:"title"`
	State   string `json:"state"`
	Type    string `json:"type"`
	StageID string `json:"stageId"`
}

// graphViewStages returns the stages of a build read from Pipeline Graph View, flattened with
// the ids of their children.
func (w *walker) graphViewStages(ctx context.Context, buildPath, buildURL string) ([]*stageEntry, error) {
	var tree graphTreeResponse
	if _, err := w.client.getJSON(ctx, buildPath+graphViewTreePath, "", &tree); err != nil {
		return nil, err
	}
	if tree.Status != graphViewStatusOK {
		return nil, fmt.Errorf("pipeline graph view returned status %q for %s", tree.Status, buildPath)
	}

	entries := flattenGraphStages(tree.Data.Stages)
	failed := failedLeaves(entries)
	if len(failed) == 0 {
		return entries, nil
	}

	steps := w.graphViewSteps(ctx, buildPath)
	for _, entry := range failed {
		entry.failure = w.graphViewFailure(ctx, buildPath, buildURL, entry, steps)
	}

	return entries, nil
}

// graphViewSteps returns every step of a build, or nil when they cannot be read.
func (w *walker) graphViewSteps(ctx context.Context, buildPath string) []graphStep {
	var steps graphStepsResponse
	if _, err := w.client.getJSON(ctx, buildPath+graphViewAllStepsPath, "", &steps); err != nil {
		if ctx.Err() == nil {
			w.log.Warn("failed to read the steps of the build", "build", buildPath, "error", err.Error())
		}
		return nil
	}
	return steps.Data.Steps
}

// graphViewFailure returns the failure details of a failed stage. The failed step is taken from
// the steps of the build; the error comes from Stage View when it is available, because it
// exposes message and type separately, and from the exception text of Graph View otherwise.
func (w *walker) graphViewFailure(ctx context.Context, buildPath, buildURL string, entry *stageEntry, steps []graphStep) map[string]any {
	failure := newFailure(buildURL)

	nodeID := entry.id
	if step := failedStep(steps, entry.id); step != nil {
		nodeID = step.ID
		if step.Type == graphViewStepType {
			setFailedStep(failure, buildURL, step.ID, stepName(step))
		} else {
			failure["nodeName"] = stepName(step)
		}
	}

	if w.stageAPIs.stageView != apiUnavailable {
		if described, ok := w.stageViewNodeError(ctx, buildPath, nodeID); ok {
			setFailureError(failure, described.message, described.errType)
			if _, hasStep := failure["nodeName"]; !hasStep && described.stepID != "" {
				setFailedStep(failure, buildURL, described.stepID, described.stepName)
			}
			return failure
		}
	}

	query := url.Values{}
	query.Set(graphViewNodeIDParam, nodeID)
	text, err := w.client.getText(ctx, buildPath+graphViewExceptionTextPath, query, maxExceptionTextSize)
	if err != nil {
		if ctx.Err() == nil {
			w.log.Warn("failed to read the exception text of a failed stage", "build", buildURL, "stage", entry.name, "error", err.Error())
		}
		return failure
	}

	message, errType := parseExceptionText(text)
	setFailureError(failure, message, errType)
	return failure
}

// flattenGraphStages flattens the tree of stages in execution order, each parent before its
// children. The children of a stage are its children followed by the chain of their next
// siblings; every stage appears once, even when it is referenced more than once.
func flattenGraphStages(stages []graphStage) []*stageEntry {
	var entries []*stageEntry
	seen := make(map[string]bool)

	var visit func(stage *graphStage)
	visit = func(stage *graphStage) {
		if stage.ID == "" || seen[stage.ID] {
			return
		}
		seen[stage.ID] = true

		children := expandSiblings(stage.Children)
		entry := &stageEntry{
			id:              stage.ID,
			name:            stage.Name,
			status:          graphViewStatus(stage.State),
			startTimeMillis: stage.StartTimeMillis,
			durationMillis:  stage.TotalDurationMillis,
			agent:           stage.Agent,
			parallel:        strings.EqualFold(stage.Type, graphViewParallelType),
			synthetic:       stage.Synthetic,
			children:        make([]string, 0, len(children)),
		}
		for _, child := range children {
			entry.children = append(entry.children, child.ID)
		}
		entries = append(entries, entry)

		for _, child := range children {
			visit(child)
		}
	}

	for _, root := range expandSiblings(stages) {
		visit(root)
	}

	return entries
}

// expandSiblings returns stages followed, each, by the chain of its next siblings, without
// duplicates and without stages lacking an id.
func expandSiblings(stages []graphStage) []*graphStage {
	out := make([]*graphStage, 0, len(stages))
	seen := make(map[string]bool)
	for idx := range stages {
		for current := &stages[idx]; current != nil && current.ID != "" && !seen[current.ID]; current = current.NextSibling {
			seen[current.ID] = true
			out = append(out, current)
		}
	}
	return out
}

// failedLeaves returns the failed stages none of whose children failed: the failure details are
// attached to the innermost failed stage only.
func failedLeaves(entries []*stageEntry) []*stageEntry {
	byID := make(map[string]*stageEntry, len(entries))
	for _, entry := range entries {
		byID[entry.id] = entry
	}

	var leaves []*stageEntry
	for _, entry := range entries {
		if entry.status != stageStatusFailure {
			continue
		}
		childFailed := false
		for _, childID := range entry.children {
			if child, ok := byID[childID]; ok && child.status == stageStatusFailure {
				childFailed = true
				break
			}
		}
		if !childFailed {
			leaves = append(leaves, entry)
		}
	}
	return leaves
}

// failedStep returns the failed step of the stage, preferring a step to an error thrown outside
// any step, or nil when there is none.
func failedStep(steps []graphStep, stageID string) *graphStep {
	var unhandled *graphStep
	for idx := range steps {
		step := &steps[idx]
		if step.StageID != stageID || graphViewStatus(step.State) != stageStatusFailure {
			continue
		}
		if step.Type == graphViewStepType {
			return step
		}
		if unhandled == nil {
			unhandled = step
		}
	}
	return unhandled
}

// stepName returns the descriptive name of a step, falling back to its arguments or label when
// the step has no descriptive name, as for Shell Script.
func stepName(step *graphStep) string {
	if step.Title != "" {
		return step.Title
	}
	return step.Name
}

// graphViewStatus returns the common status of a Pipeline Graph View state, which already uses
// the common vocabulary.
func graphViewStatus(state string) string {
	if state == "" {
		return stageStatusUnknown
	}
	return strings.ToLower(state)
}

// parseExceptionText splits the exception text of Pipeline Graph View into message and type.
// The stack trace, from the first line starting with "at ", is dropped. When the remaining text
// starts with a fully qualified Java class name followed by a colon or by the end of the text,
// the class name is the type and the rest is the message; otherwise the whole text is the message
// and the type is empty.
func parseExceptionText(text string) (string, string) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "at ") {
			break
		}
		kept = append(kept, line)
	}

	head := strings.TrimSpace(strings.Join(kept, "\n"))
	if match := exceptionHeadPattern.FindStringSubmatch(head); match != nil {
		return strings.TrimSpace(match[2]), match[1]
	}
	return head, ""
}
