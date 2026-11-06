// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package jenkins

import (
	"context"
	"fmt"

	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/mia-platform/ibdm/internal/source"
)

const (
	folderClass                = "com.cloudbees.hudson.plugins.folder.Folder"
	organizationFolderClass    = "jenkins.branch.OrganizationFolder"
	multibranchClass           = "org.jenkinsci.plugins.workflow.multibranch.WorkflowMultiBranchProject"
	workflowJobClass           = "org.jenkinsci.plugins.workflow.job.WorkflowJob"
	objectMetadataActionClass  = "jenkins.scm.api.metadata.ObjectMetadataAction"
	primaryInstanceActionClass = "jenkins.scm.api.metadata.PrimaryInstanceMetadataAction"

	parentKindInstance = "instance"
	parentKindFolder   = "folder"

	folderKindFolder       = "folder"
	folderKindOrganization = "organization"

	workflowKindStandalone    = "standalone"
	workflowKindPrimaryBranch = "primaryBranch"

	usageKindWorkflow    = "workflow"
	usageKindMultibranch = "multibranch"

	containerKindFolder      = "folder"
	containerKindMultibranch = "multibranch"

	// Keys shared by the values of several data types.
	keyName        = "name"
	keyFullName    = "fullName"
	keyURL         = "url"
	keyKind        = "kind"
	keyDescription = "description"

	// maxFolderDepth bounds the folder recursion as a safety net against unexpectedly deep trees.
	maxFolderDepth = 20

	// buildTree selects the fields of a build, including the shared libraries it loaded.
	buildTree = "number,result,timestamp,duration,url,actions[_class,libraries[name,version,trusted,changelog,variables]]"
	// jobTree selects the fields of any item of the folder tree; jobs[name] is only present on
	// containers and is used to recognise them.
	jobTree = "_class,name,fullName,displayName,description,url,buildable," +
		"healthReport[score,description],actions[_class,objectUrl]," +
		"lastCompletedBuild[" + buildTree + "],jobs[name]"
	// containerTree selects the children of a container.
	containerTree = "jobs[" + jobTree + "]"
)

// jobItem is an item of the folder tree: a folder, a multibranch project or a job.
type jobItem struct {
	Class              string         `json:"_class"` //nolint:tagliatelle // Jenkins field name
	Name               string         `json:"name"`
	FullName           string         `json:"fullName"`
	DisplayName        string         `json:"displayName"`
	Description        string         `json:"description"`
	URL                string         `json:"url"`
	Buildable          bool           `json:"buildable"`
	HealthReport       []healthReport `json:"healthReport"`
	Actions            []jobAction    `json:"actions"`
	LastCompletedBuild *buildInfo     `json:"lastCompletedBuild"`
	// Jobs is non-nil only for containers.
	Jobs []jobRef `json:"jobs"`
}

// jobAction is an action attached to an item; only the fields used by the source are decoded.
type jobAction struct {
	Class     string `json:"_class"` //nolint:tagliatelle // Jenkins field name
	ObjectURL string `json:"objectUrl"`
}

// healthReport is an entry of the weather report of an item.
type healthReport struct {
	Score       int    `json:"score"`
	Description string `json:"description"`
}

// jobRef is the name of a child of a container.
type jobRef struct {
	Name string `json:"name"`
}

// buildInfo is a completed build of a workflow.
type buildInfo struct {
	Number    int64         `json:"number"`
	Result    string        `json:"result"`
	Timestamp int64         `json:"timestamp"`
	Duration  int64         `json:"duration"`
	URL       string        `json:"url"`
	Actions   []buildAction `json:"actions"`
}

// buildAction is an action attached to a build; only the shared library records are decoded.
type buildAction struct {
	Libraries []libraryRecord `json:"libraries"`
}

// containerListing is the response listing the children of a container.
type containerListing struct {
	Jobs []jobItem `json:"jobs"`
}

// parentRef identifies the parent of an item: the controller or a folder.
type parentRef struct {
	kind     string
	fullName string
	url      string
}

// values returns the parent reference as exposed to the mappings.
func (p parentRef) values() map[string]any {
	return map[string]any{
		keyKind:     p.kind,
		keyFullName: p.fullName,
		keyURL:      p.url,
	}
}

// container is a folder or a multibranch project, which may own a folder credentials store.
type container struct {
	kind     string
	segments []string
	fullName string
	url      string
}

// values returns the reference to the container as exposed to the mappings.
func (c *container) values() map[string]any {
	return map[string]any{
		keyKind:     c.kind,
		keyFullName: c.fullName,
		keyURL:      c.url,
	}
}

// indexEntry describes a workflow or a multibranch project known to the sync, with its parent,
// used to resolve the usages of the credentials.
type indexEntry struct {
	kind     string
	fullName string
	url      string
	parent   parentRef
}

// inventory holds what the walk of the folder tree collected for the later steps of the sync.
type inventory struct {
	containers []container
	index      map[string]indexEntry
	libraries  *libraryCollector
}

// walkOptions selects what the walk of the folder tree emits and collects.
type walkOptions struct {
	emitFolders      bool
	emitMultibranch  bool
	emitWorkflows    bool
	collectLibraries bool
}

// needsPrimaryBranch reports whether the primary branch job of the multibranch projects is needed.
func (o walkOptions) needsPrimaryBranch() bool {
	return o.emitWorkflows || o.collectLibraries
}

// walker walks the folder tree of the controller, emitting items and filling the inventory.
type walker struct {
	client  *client
	results chan<- source.Data
	opts    walkOptions
	inv     *inventory
	log     logger.Logger

	// stageAPIs records which stage plugins are usable, for the whole walk.
	stageAPIs stageAPIs
}

// walkJobs walks the whole folder tree of the controller. A failure listing the root is
// returned; failures on nested containers are logged and the walk continues.
func (s *Source) walkJobs(ctx context.Context, opts walkOptions, results chan<- source.Data) (*inventory, error) {
	w := &walker{
		client:  s.client,
		results: results,
		opts:    opts,
		inv: &inventory{
			index:     make(map[string]indexEntry),
			libraries: newLibraryCollector(),
		},
		log: logger.FromContext(ctx).WithName(loggerName),
	}

	root := parentRef{kind: parentKindInstance, url: s.client.baseURL + "/"}
	if err := w.walkContainer(ctx, nil, root, 0); err != nil {
		return nil, err
	}

	return w.inv, nil
}

// walkContainer lists the children of the container identified by segments and handles each of
// them. The root of the controller is identified by empty segments.
func (w *walker) walkContainer(ctx context.Context, segments []string, parent parentRef, depth int) error {
	var listing containerListing
	if _, err := w.client.getJSON(ctx, jobPath(segments)+apiJSONPath, containerTree, &listing); err != nil {
		return err
	}

	w.log.Trace("listed container", "path", jobPath(segments), "children", len(listing.Jobs))
	for _, item := range listing.Jobs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := w.handleItem(ctx, item, appendSegment(segments, item.Name), parent, depth); err != nil {
			return err
		}
	}

	return nil
}

// handleItem dispatches an item of the folder tree by its class. Only context errors are
// returned: any other failure is logged and the walk continues.
func (w *walker) handleItem(ctx context.Context, item jobItem, segments []string, parent parentRef, depth int) error {
	switch {
	case item.Class == folderClass || item.Class == organizationFolderClass:
		return w.handleFolder(ctx, item, segments, parent, depth)
	case item.Class == multibranchClass:
		return w.handleMultibranch(ctx, item, segments, parent)
	case item.Class == workflowJobClass:
		w.inv.index[item.FullName] = indexEntry{kind: usageKindWorkflow, fullName: item.FullName, url: item.URL, parent: parent}
		return w.handleWorkflow(ctx, item, segments, workflowKindStandalone, &parent, nil)
	case item.Jobs != nil:
		w.log.Debug("entering container of unknown kind", "fullName", item.FullName, "class", item.Class)
		return w.descend(ctx, item, segments, parent, depth)
	default:
		w.log.Debug("skipping item of unsupported kind", "fullName", item.FullName, "class", item.Class)
		return nil
	}
}

// handleFolder emits a folder or an organization folder and walks its children.
func (w *walker) handleFolder(ctx context.Context, item jobItem, segments []string, parent parentRef, depth int) error {
	w.inv.containers = append(w.inv.containers, container{kind: containerKindFolder, segments: segments, fullName: item.FullName, url: item.URL})

	if w.opts.emitFolders {
		if err := send(ctx, w.results, folderType, folderValues(item, parent)); err != nil {
			return err
		}
	}

	folderParent := parentRef{kind: parentKindFolder, fullName: item.FullName, url: item.URL}
	return w.descend(ctx, item, segments, folderParent, depth)
}

// descend walks the children of a container one level deeper, within maxFolderDepth.
func (w *walker) descend(ctx context.Context, item jobItem, segments []string, parent parentRef, depth int) error {
	if depth+1 >= maxFolderDepth {
		w.log.Warn("maximum folder depth reached, skipping children", "fullName", item.FullName)
		return nil
	}

	if err := w.walkContainer(ctx, segments, parent, depth+1); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		w.log.Error("failed to list folder, skipping", "fullName", item.FullName, "error", err.Error())
	}

	return nil
}

// handleWorkflow collects the shared libraries of a workflow and emits it with the stages of
// its last completed build. Standalone workflows carry their parent, primary branch workflows
// carry their multibranch project.
func (w *walker) handleWorkflow(ctx context.Context, item jobItem, segments []string, kind string, parent *parentRef, multibranch map[string]any) error {
	build := item.LastCompletedBuild
	if w.opts.collectLibraries && build != nil {
		w.inv.libraries.add(item, kind, multibranch, build)
	}

	if !w.opts.emitWorkflows {
		return nil
	}

	values := map[string]any{
		"workflow": map[string]any{
			keyName:        item.Name,
			keyFullName:    item.FullName,
			"displayName":  item.DisplayName,
			keyDescription: item.Description,
			keyURL:         item.URL,
			"buildable":    item.Buildable,
			keyKind:        kind,
		},
		"libraries": workflowLibraries(build),
	}
	if parent != nil {
		values["parent"] = parent.values()
	}
	if multibranch != nil {
		values["multibranch"] = multibranch
	}
	if build != nil {
		values["build"] = w.buildValues(ctx, segments, build)
	}

	return send(ctx, w.results, workflowType, values)
}

// buildValues returns the last completed build of a workflow with its stages, when available.
func (w *walker) buildValues(ctx context.Context, segments []string, build *buildInfo) map[string]any {
	values := map[string]any{
		"number":    build.Number,
		"result":    build.Result,
		"timestamp": build.Timestamp,
		"duration":  build.Duration,
		keyURL:      build.URL,
	}

	if stages, stagesSource, ok := w.fetchStages(ctx, segments, build); ok {
		values["stages"] = stages
		values["stagesSource"] = stagesSource
	}

	return values
}

// folderValues returns the values of a folder or organization folder.
func folderValues(item jobItem, parent parentRef) map[string]any {
	kind := folderKindFolder
	if item.Class == organizationFolderClass {
		kind = folderKindOrganization
	}

	return map[string]any{
		"folder": map[string]any{
			keyName:        item.Name,
			keyFullName:    item.FullName,
			"displayName":  item.DisplayName,
			keyDescription: item.Description,
			keyURL:         item.URL,
			keyKind:        kind,
			"objectUrl":    objectURL(item.Actions),
		},
		"parent": parent.values(),
	}
}

// objectURL returns the external URL of the object an item was created from, such as the
// repository of a multibranch project, or an empty string when the item has none.
func objectURL(actions []jobAction) string {
	for _, action := range actions {
		if action.Class == objectMetadataActionClass && action.ObjectURL != "" {
			return action.ObjectURL
		}
	}
	return ""
}

// hasAction reports whether actions hold an action of the given class.
func hasAction(actions []jobAction, class string) bool {
	for _, action := range actions {
		if action.Class == class {
			return true
		}
	}
	return false
}

// healthValues returns the health report entries of an item.
func healthValues(reports []healthReport) []map[string]any {
	out := make([]map[string]any, 0, len(reports))
	for _, report := range reports {
		out = append(out, map[string]any{"score": report.Score, keyDescription: report.Description})
	}
	return out
}

// send pushes an upsert of dataType onto results, giving up when ctx is done.
func send(ctx context.Context, results chan<- source.Data, dataType string, values map[string]any) error {
	data := source.Data{
		Type:      dataType,
		Operation: source.DataOperationUpsert,
		Values:    values,
		Time:      timeSource(),
	}

	select {
	case <-ctx.Done():
		return fmt.Errorf("sending %s: %w", dataType, ctx.Err())
	case results <- data:
		return nil
	}
}
