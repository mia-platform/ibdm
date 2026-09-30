// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package sonarqube

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/mia-platform/ibdm/internal/logger"
)

const (
	// sonarqubeMaxPageSize is the upper bound SonarQube enforces on the `ps`
	// parameter of its paginated search endpoints.
	sonarqubeMaxPageSize = 500

	issuesSearchPath     = "/api/issues/search"
	projectLinksPath     = "/api/project_links/search"
	componentsSearchPath = "/api/components/search"
	componentsShowPath   = "/api/components/show"
	projectBranchesPath  = "/api/project_branches/list"
	projectAnalysesPath  = "/api/project_analyses/search"

	qualityGateStatusPath    = "/api/qualitygates/project_status"
	qualityGateByProjectPath = "/api/qualitygates/get_by_project"

	// additionalFields asks for the rules block alongside the issues, so an
	// item can name its rule and not only cite its key.
	additionalFields = "rules"
	// scmLinkType is the project link type produced by sonar.links.scm.
	scmLinkType = "scm"
	// projectQualifier selects projects in /api/components/search.
	projectQualifier = "TRK"

	// maxPaginatedResults is how deep /api/issues/search paginates before refusing.
	maxPaginatedResults = 10_000

	// maxErrorBodySize limits how many bytes are read from error response bodies.
	maxErrorBodySize = 1024
)

var (
	// impactSeverities are the buckets a project past maxPaginatedResults is
	// read in, worst first, so that a read still truncated keeps what matters.
	impactSeverities = []string{"BLOCKER", "HIGH", "MEDIUM", "LOW", "INFO"}
)

// client reads one SonarQube server.
type client struct {
	baseURL       string
	token         string
	issueStatuses string
	newCodeOnly   bool
	pageSize      int
	maxIssues     int

	httpClient *http.Client
}

// newClient builds a client from the source configuration.
func newClient(cfg config) *client {
	return &client{
		baseURL:       cfg.URL,
		token:         cfg.Token,
		issueStatuses: cfg.IssueStatuses,
		newCodeOnly:   cfg.NewCodeOnly,
		pageSize:      sonarqubeMaxPageSize,
		maxIssues:     cfg.MaxIssues,
		httpClient: &http.Client{
			Timeout: cfg.HTTPTimeout,
		},
	}
}

// get performs an authenticated GET and decodes a successful JSON answer into out.
func (c *client) get(ctx context.Context, path string, query url.Values, out any) error {
	rawURL := c.baseURL + path
	if len(query) > 0 {
		rawURL += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fmt.Errorf("creating request for %s: %w", path, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("executing request for %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodySize))
		return &apiError{path: path, statusCode: resp.StatusCode, body: strings.TrimSpace(string(body))}
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decoding response for %s: %w", path, err)
	}

	return nil
}

// apiError is a non-2xx answer from the SonarQube Web API.
type apiError struct {
	path       string
	statusCode int
	body       string
}

// Error implements error.
func (e *apiError) Error() string {
	return fmt.Sprintf("SonarQube API %s returned status %d: %s", e.path, e.statusCode, e.body)
}

// searchResponse is one page of /api/issues/search.
type searchResponse struct {
	Paging *searchPaging `json:"paging"`
	// Total is where servers older than the paging block report the count.
	Total  *int             `json:"total"`
	Issues []map[string]any `json:"issues"`
	Rules  []map[string]any `json:"rules"`
}

// searchPaging is the pagination block of a search response.
type searchPaging struct {
	Total *int `json:"total"`
}

// total returns how many issues match the query, from whichever field the server used.
func (r *searchResponse) total() int {
	if r.Paging != nil && r.Paging.Total != nil {
		return *r.Paging.Total
	}
	if r.Total != nil {
		return *r.Total
	}
	return 0
}

// analysisIssues holds the findings of one analysis.
type analysisIssues struct {
	// issues are de-duplicated by issue key.
	issues []map[string]any
	// rules are the rules the issues cite, by rule key.
	rules map[string]map[string]any
	// truncated reports that SonarQube holds more issues than were read,
	// because SONARQUBE_MAX_ISSUES was hit or a severity bucket is itself past
	// the pagination ceiling.
	truncated bool

	seen map[string]struct{}
}

// issues reads every issue the project ref currently has in the configured statuses.
//
// /api/issues/search refuses to paginate past 10 000 results. Past that the
// query is split per impact severity. Buckets overlap (an issue can be LOW for
// maintainability and HIGH for security), so results are de-duplicated by key.
// A partial read is not an error: it comes back with truncated set.
func (c *client) issues(ctx context.Context, projectKey string, scope componentScope) (*analysisIssues, error) {
	log := logger.FromContext(ctx).WithName(loggerName)
	result := &analysisIssues{
		rules: make(map[string]map[string]any),
		seen:  make(map[string]struct{}),
	}

	probe, err := c.search(ctx, projectKey, scope, "", 1)
	if err != nil {
		return nil, err
	}
	total := probe.total()

	if total <= maxPaginatedResults {
		// The probe is the first page; more are only needed when it could not
		// hold every result.
		if c.absorb(ctx, probe, result) && c.pageSize < total {
			if err := c.collectPages(ctx, projectKey, scope, "", 2, result); err != nil {
				return nil, err
			}
		}
		return result, nil
	}

	log.Warn("the project has more issues than one query can paginate; reading it per severity", "project", projectKey, "total", total)
	for _, severity := range impactSeverities {
		if err := c.collectPages(ctx, projectKey, scope, severity, 1, result); err != nil {
			return nil, err
		}
		if result.truncated {
			break
		}
	}

	return result, nil
}

// collectPages walks pages from startPage until the result set, the API
// ceiling or the configured one is exhausted.
func (c *client) collectPages(ctx context.Context, projectKey string, scope componentScope, severity string, startPage int, result *analysisIssues) error {
	log := logger.FromContext(ctx).WithName(loggerName)
	lastPage := maxPaginatedResults / c.pageSize

	for page := startPage; ; page++ {
		if page > lastPage {
			// Only reachable for a severity bucket that is itself over the ceiling.
			result.truncated = true
			log.Warn("the severity bucket is itself past the pagination ceiling; it is truncated", "project", projectKey, "severity", severity)
			return nil
		}

		response, err := c.search(ctx, projectKey, scope, severity, page)
		if err != nil {
			return err
		}
		returned := len(response.Issues)
		total := response.total()

		if !c.absorb(ctx, response, result) {
			return nil
		}
		if returned == 0 || page*c.pageSize >= total {
			return nil
		}
	}
}

// absorb folds a page into result and reports whether there is room for more.
func (c *client) absorb(ctx context.Context, response *searchResponse, result *analysisIssues) bool {
	for _, rule := range response.Rules {
		if key, _ := rule["key"].(string); key != "" {
			if _, found := result.rules[key]; !found {
				result.rules[key] = rule
			}
		}
	}

	for _, issue := range response.Issues {
		key, _ := issue["key"].(string)
		if _, duplicate := result.seen[key]; duplicate {
			continue
		}
		if len(result.issues) >= c.maxIssues {
			result.truncated = true
			logger.FromContext(ctx).WithName(loggerName).Warn("reached SONARQUBE_MAX_ISSUES; the rest of this analysis is not recorded", "limit", c.maxIssues)
			return false
		}
		result.seen[key] = struct{}{}
		result.issues = append(result.issues, issue)
	}

	return true
}

// search performs one /api/issues/search request.
func (c *client) search(ctx context.Context, projectKey string, scope componentScope, severity string, page int) (*searchResponse, error) {
	query := url.Values{}
	query.Set("components", projectKey)
	query.Set("issueStatuses", c.issueStatuses)
	query.Set("additionalFields", additionalFields)
	query.Set("ps", strconv.Itoa(c.pageSize))
	query.Set("p", strconv.Itoa(page))
	scope.apply(query)
	if c.newCodeOnly {
		query.Set("inNewCodePeriod", "true")
	}
	if severity != "" {
		query.Set("impactSeverities", severity)
	}

	var response searchResponse
	if err := c.get(ctx, issuesSearchPath, query, &response); err != nil {
		return nil, fmt.Errorf("searching issues of project %q: %w", projectKey, err)
	}
	return &response, nil
}

// projectRepositoryURL returns the repository URL the project declares through
// sonar.links.scm. It never fails: this is one optional field on every item,
// and a token that cannot read project links must not cost the findings.
// Deliberately not cached, so that fixing the link in SonarQube takes effect
// on the next analysis.
func (c *client) projectRepositoryURL(ctx context.Context, projectKey string) string {
	log := logger.FromContext(ctx).WithName(loggerName)

	var response struct {
		Links []struct {
			Type string `json:"type"`
			URL  string `json:"url"`
		} `json:"links"`
	}
	if err := c.get(ctx, projectLinksPath, url.Values{"projectKey": {projectKey}}, &response); err != nil {
		log.Warn("reading the project links failed; items carry no SCM link unless another source supplies one", "project", projectKey, "error", err.Error())
		return ""
	}

	for _, link := range response.Links {
		if strings.EqualFold(link.Type, scmLinkType) {
			return link.URL
		}
	}
	return ""
}

// project is a SonarQube project, as listed for a sync.
type project struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

// listProjects returns every project the token can browse.
func (c *client) listProjects(ctx context.Context) ([]project, error) {
	projects := make([]project, 0)
	for page := 1; ; page++ {
		var response struct {
			Paging     searchPaging `json:"paging"`
			Components []project    `json:"components"`
		}
		query := url.Values{
			"qualifiers": {projectQualifier},
			"ps":         {strconv.Itoa(sonarqubeMaxPageSize)},
			"p":          {strconv.Itoa(page)},
		}
		if err := c.get(ctx, componentsSearchPath, query, &response); err != nil {
			return nil, fmt.Errorf("listing projects: %w", err)
		}

		projects = append(projects, response.Components...)

		total := 0
		if response.Paging.Total != nil {
			total = *response.Paging.Total
		}
		if len(response.Components) == 0 || page*sonarqubeMaxPageSize >= total {
			return projects, nil
		}
	}
}

// getProject returns one project by key.
func (c *client) getProject(ctx context.Context, projectKey string) (project, error) {
	var response struct {
		Component project `json:"component"`
	}
	if err := c.get(ctx, componentsShowPath, url.Values{"component": {projectKey}}, &response); err != nil {
		return project{}, fmt.Errorf("reading project %q: %w", projectKey, err)
	}
	return response.Component, nil
}

// branchInfo is a project branch as listed by /api/project_branches/list.
type branchInfo struct {
	Name         string `json:"name"`
	IsMain       bool   `json:"isMain"`
	AnalysisDate string `json:"analysisDate"`
}

// analysisInfo is a project analysis as listed by /api/project_analyses/search.
type analysisInfo struct {
	Key            string `json:"key"`
	Date           string `json:"date"`
	Revision       string `json:"revision"`
	ProjectVersion string `json:"projectVersion"`
}

// errNoAnalysis reports a project that was never analysed.
var errNoAnalysis = errors.New("no analysis listed")

// latestAnalysis returns the most recent analysis of the main branch of a project.
func (c *client) latestAnalysis(ctx context.Context, projectKey string) (analysisInfo, error) {
	var response struct {
		Analyses []analysisInfo `json:"analyses"`
	}
	query := url.Values{"project": {projectKey}, "ps": {"1"}}
	if err := c.get(ctx, projectAnalysesPath, query, &response); err != nil {
		return analysisInfo{}, fmt.Errorf("listing analyses of project %q: %w", projectKey, err)
	}
	if len(response.Analyses) == 0 {
		return analysisInfo{}, fmt.Errorf("project %q: %w", projectKey, errNoAnalysis)
	}
	return response.Analyses[0], nil
}

// qualityGateStatus is the verdict of the quality gate on one analysis.
type qualityGateStatus struct {
	Status     string           `json:"status"`
	Conditions []map[string]any `json:"conditions"`
}

// analysisQualityGate returns the quality gate status of one analysis.
func (c *client) analysisQualityGate(ctx context.Context, analysisKey string) (qualityGateStatus, error) {
	var response struct {
		ProjectStatus qualityGateStatus `json:"projectStatus"`
	}
	if err := c.get(ctx, qualityGateStatusPath, url.Values{"analysisId": {analysisKey}}, &response); err != nil {
		return qualityGateStatus{}, fmt.Errorf("reading the quality gate status of analysis %q: %w", analysisKey, err)
	}
	return response.ProjectStatus, nil
}

// qualityGateName returns the name of the quality gate a project uses.
func (c *client) qualityGateName(ctx context.Context, projectKey string) (string, error) {
	var response struct {
		QualityGate struct {
			Name string `json:"name"`
		} `json:"qualityGate"`
	}
	if err := c.get(ctx, qualityGateByProjectPath, url.Values{"project": {projectKey}}, &response); err != nil {
		return "", fmt.Errorf("reading the quality gate of project %q: %w", projectKey, err)
	}
	return response.QualityGate.Name, nil
}

// errNoMainBranch reports a project whose branch list names no main branch.
var errNoMainBranch = errors.New("no main branch listed")

// mainBranch returns the main branch of a project.
func (c *client) mainBranch(ctx context.Context, projectKey string) (branchInfo, error) {
	var response struct {
		Branches []branchInfo `json:"branches"`
	}
	if err := c.get(ctx, projectBranchesPath, url.Values{"project": {projectKey}}, &response); err != nil {
		return branchInfo{}, fmt.Errorf("listing branches of project %q: %w", projectKey, err)
	}
	for _, branch := range response.Branches {
		if branch.IsMain {
			return branch, nil
		}
	}
	return branchInfo{}, fmt.Errorf("project %q: %w", projectKey, errNoMainBranch)
}
