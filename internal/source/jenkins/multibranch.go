// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package jenkins

import (
	"context"
)

const (
	// multibranchTree selects the children of a multibranch project with only the fields needed
	// to find the primary branch, and the views grouping branches, pull requests and tags.
	multibranchTree = "jobs[_class,name,fullName,url,actions[_class]],views[name,jobs[name]]"

	branchesViewName      = "default"
	pullRequestsViewName  = "change-requests"
	tagsViewName          = "tags"
	branchCountKey        = "branchCount"
	pullRequestCountKey   = "pullRequestCount"
	tagCountKey           = "tagCount"
	multibranchValuesKey  = "multibranch"
	primaryBranchValueKey = "primaryBranch"
)

// viewCountKeys binds the views of a multibranch project to the counters exposed to the mappings.
var viewCountKeys = map[string]string{
	branchesViewName:     branchCountKey,
	pullRequestsViewName: pullRequestCountKey,
	tagsViewName:         tagCountKey,
}

// multibranchListing is the response listing the branches of a multibranch project.
type multibranchListing struct {
	Jobs  []branchItem `json:"jobs"`
	Views []viewItem   `json:"views"`
}

// branchItem is a branch, pull request or tag job of a multibranch project.
type branchItem struct {
	Class    string      `json:"_class"` //nolint:tagliatelle // Jenkins field name
	Name     string      `json:"name"`
	FullName string      `json:"fullName"`
	URL      string      `json:"url"`
	Actions  []jobAction `json:"actions"`
}

// viewItem is a view of a multibranch project with the names of the jobs it shows.
type viewItem struct {
	Name string   `json:"name"`
	Jobs []jobRef `json:"jobs"`
}

// handleMultibranch emits a multibranch project and its primary branch workflow. Branches,
// pull requests and tags other than the primary branch are never emitted.
func (w *walker) handleMultibranch(ctx context.Context, item jobItem, segments []string, parent parentRef) error {
	w.inv.containers = append(w.inv.containers, container{segments: segments, fullName: item.FullName, url: item.URL})
	w.inv.index[item.FullName] = indexEntry{kind: usageKindMultibranch, fullName: item.FullName, url: item.URL, folder: parent.fullName}

	if !w.opts.emitMultibranch && !w.opts.needsPrimaryBranch() {
		return nil
	}

	var listing *multibranchListing
	var fetched multibranchListing
	if _, err := w.client.getJSON(ctx, jobPath(segments)+apiJSONPath, multibranchTree, &fetched); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		w.log.Error("failed to list multibranch branches", "fullName", item.FullName, "error", err.Error())
	} else {
		listing = &fetched
	}

	primary := primaryBranch(listing)
	if w.opts.emitMultibranch {
		if err := send(ctx, w.results, multibranchType, multibranchValues(item, parent, listing, primary)); err != nil {
			return err
		}
	}

	if primary == nil {
		w.log.Debug("multibranch project without primary branch", "fullName", item.FullName)
		return nil
	}

	if !w.opts.needsPrimaryBranch() {
		return nil
	}

	return w.handlePrimaryBranch(ctx, item, segments, primary)
}

// handlePrimaryBranch reads the primary branch job of a multibranch project and handles it as
// a workflow bound to the project.
func (w *walker) handlePrimaryBranch(ctx context.Context, project jobItem, segments []string, primary *branchItem) error {
	var job jobItem
	if _, err := w.client.getJSON(ctx, jobPath(appendSegment(segments, primary.Name))+apiJSONPath, jobTree, &job); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		w.log.Error("failed to read primary branch, skipping", "fullName", primary.FullName, "error", err.Error())
		return nil
	}

	multibranch := map[string]any{
		keyName:     project.Name,
		keyFullName: project.FullName,
		keyURL:      project.URL,
	}
	return w.handleWorkflow(ctx, job, appendSegment(segments, primary.Name), workflowKindPrimaryBranch, nil, multibranch)
}

// primaryBranch returns the branch job marked as the primary instance of the multibranch
// project, or nil when the listing is missing or no branch is marked.
func primaryBranch(listing *multibranchListing) *branchItem {
	if listing == nil {
		return nil
	}

	for idx := range listing.Jobs {
		if hasAction(listing.Jobs[idx].Actions, primaryInstanceActionClass) {
			return &listing.Jobs[idx]
		}
	}
	return nil
}

// multibranchValues returns the values of a multibranch project. Branch information is added
// only when the branches could be listed.
func multibranchValues(item jobItem, parent parentRef, listing *multibranchListing, primary *branchItem) map[string]any {
	project := map[string]any{
		keyName:         item.Name,
		keyFullName:     item.FullName,
		"displayName":   item.DisplayName,
		"description":   item.Description,
		keyURL:          item.URL,
		"health":        healthValues(item.HealthReport),
		"repositoryUrl": objectURL(item.Actions),
	}

	if primary != nil {
		project[primaryBranchValueKey] = map[string]any{
			keyName:     primary.Name,
			keyFullName: primary.FullName,
			keyURL:      primary.URL,
		}
	}

	if listing != nil {
		for _, view := range listing.Views {
			if key, ok := viewCountKeys[view.Name]; ok {
				project[key] = len(view.Jobs)
			}
		}
	}

	return map[string]any{
		multibranchValuesKey: project,
		"parent":             parent.values(),
	}
}
