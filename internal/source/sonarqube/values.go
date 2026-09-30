// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package sonarqube

import (
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// labelPrefix prefixes every label key in derived.labels.
	labelPrefix = "sonarqube.mia-platform.eu/"
	// baseTag is carried by every item, so "everything SonarQube reported" is one query.
	baseTag = "sonarqube"

	// projectSlugLimit is how much of the project key the item name keeps.
	projectSlugLimit = 40
	// projectSlugFallback is used when a project key sanitises away to nothing.
	projectSlugFallback = "project"
	// titleLimit is the longest title written, in characters.
	titleLimit = 200
	// labelValueLimit is the longest value a Catalog label or tag may carry.
	labelValueLimit = 63

	sonarqubeLinkTitle = "Open in SonarQube"
	scmLinkTitle       = "Open in the repository"
)

// sonarqubeTimeLayouts are the timestamp spellings SonarQube uses: ISO 8601
// with an offset without colon (+0200), which is not RFC 3339, and RFC 3339.
var sonarqubeTimeLayouts = []string{"2006-01-02T15:04:05-0700", time.RFC3339}

// analysisContext is everything about an analysis that each of its issues records.
type analysisContext struct {
	serverURL         string
	projectKey        string
	projectName       string
	branch            string
	pullRequest       string
	isMainBranch      *bool
	taskID            string
	analysedAt        string
	revision          string
	qualityGateStatus string
	qualityGateName   string
	// scm is where the analysed code lives; nil when it could not be resolved.
	scm *scmTarget
	// runKey identifies the run of this analysis; empty when runs are not
	// mapped, so that issues are not related to a run that is never written.
	runKey string
}

// toValues returns the analysis as mapping values. Every key is always
// present, nil when unknown, so templates never hit a missing key.
func (a analysisContext) toValues() map[string]any {
	var isMainBranch any
	if a.isMainBranch != nil {
		isMainBranch = *a.isMainBranch
	}

	return map[string]any{
		"serverUrl":         optional(a.serverURL),
		"projectKey":        a.projectKey,
		"projectName":       optional(a.projectName),
		"branch":            optional(a.branch),
		"pullRequest":       optional(a.pullRequest),
		"isMainBranch":      isMainBranch,
		"taskId":            optional(a.taskID),
		"analysedAt":        optional(a.analysedAt),
		"revision":          optional(a.revision),
		"qualityGateStatus": optional(a.qualityGateStatus),
		"qualityGateName":   optional(a.qualityGateName),
		"runKey":            optional(a.runKey),
	}
}

// issueValues builds the mapping values of one issue:
//
//   - issue: the issue as /api/issues/search returned it
//   - rule: the rule it cites, from the rules block, or an empty object
//   - analysis: the analysis it was reported by
//   - derived: values computed here because a template cannot
func issueValues(issue map[string]any, rules map[string]map[string]any, analysis analysisContext) map[string]any {
	rule := rules[stringField(issue, "rule")]
	if rule == nil {
		rule = map[string]any{}
	}

	return map[string]any{
		"issue":    issue,
		"rule":     rule,
		"analysis": analysis.toValues(),
		"derived":  derivedValues(issue, analysis),
	}
}

// derivedValues computes what the item needs that the issue does not carry verbatim.
func derivedValues(issue map[string]any, analysis analysisContext) map[string]any {
	severity := effectiveSeverity(issue)
	status := effectiveStatus(issue)
	path := componentPath(issue, analysis.projectKey)
	line, _ := intField(issue, "line")

	var repositoryURL, scmURL string
	if analysis.scm != nil {
		repositoryURL = analysis.scm.repositoryURL
		if path != "" {
			scmURL, _ = analysis.scm.url(path, line)
		}
	}
	sonarqubeURL := sonarqubeIssueURL(issue, analysis)

	links := make([]any, 0, 2)
	if sonarqubeURL != "" {
		links = append(links, map[string]any{"title": sonarqubeLinkTitle, "url": sonarqubeURL})
	}
	if scmURL != "" {
		links = append(links, map[string]any{"title": scmLinkTitle, "url": scmURL})
	}

	effort := stringField(issue, "effort")
	if effort == "" {
		effort = stringField(issue, "debt")
	}

	return map[string]any{
		"projectSlug":   projectSlug(analysis.projectKey),
		"title":         title(issue),
		"description":   description(issue, path),
		"severity":      optional(severity),
		"status":        optional(status),
		"componentPath": optional(path),
		"effort":        optional(effort),
		"creationDate":  optional(rfc3339(stringField(issue, "creationDate"))),
		"updateDate":    optional(rfc3339(stringField(issue, "updateDate"))),
		"closeDate":     optional(rfc3339(stringField(issue, "closeDate"))),
		"sonarqubeUrl":  optional(sonarqubeURL),
		"repositoryUrl": optional(repositoryURL),
		"scmUrl":        optional(scmURL),
		"labels":        labels(issue, analysis),
		"tags":          tags(issue),
		"links":         links,
	}
}

// effectiveSeverity is the worst of the issue impacts (an issue LOW for
// maintainability and HIGH for security is a high-severity issue), or the
// legacy severity for a server reporting no impacts.
func effectiveSeverity(issue map[string]any) string {
	worst := ""
	worstRank := -1
	impacts, _ := issue["impacts"].([]any)
	for _, raw := range impacts {
		impact, _ := raw.(map[string]any)
		severity := stringField(impact, "severity")
		if severity == "" {
			continue
		}
		if rank := severityRank(severity); rank > worstRank {
			worst, worstRank = severity, rank
		}
	}
	if worst != "" {
		return worst
	}
	return stringField(issue, "severity")
}

// severityRank orders severities across both vocabularies. Unknown values
// rank lowest, so an unseen vocabulary never outranks a known one.
func severityRank(severity string) int {
	switch strings.ToUpper(severity) {
	case "BLOCKER":
		return 5 //nolint:mnd // rank of the worst severity
	case "HIGH", "CRITICAL":
		return 4 //nolint:mnd // rank below BLOCKER
	case "MEDIUM", "MAJOR":
		return 3 //nolint:mnd // rank below HIGH
	case "LOW", "MINOR":
		return 2 //nolint:mnd // rank below MEDIUM
	case "INFO":
		return 1
	default:
		return 0
	}
}

// effectiveStatus prefers the Multi-Quality Rule issueStatus over the legacy status.
func effectiveStatus(issue map[string]any) string {
	if status := stringField(issue, "issueStatus"); status != "" {
		return status
	}
	return stringField(issue, "status")
}

// componentPath is the path within the project. A component key is
// projectKey:path/to/File.java, and a project key may itself contain colons
// (Maven's groupId:artifactId), so the known project key is stripped first and
// the first colon is only a fallback. A component that is the project itself
// has no path.
func componentPath(issue map[string]any, projectKey string) string {
	component := stringField(issue, "component")
	if projectKey != "" {
		if path, found := strings.CutPrefix(component, projectKey+":"); found {
			return path
		}
		if component == projectKey {
			return ""
		}
	}

	_, path, found := strings.Cut(component, ":")
	if !found {
		return ""
	}
	return path
}

// title is what the item is called in a list. Never empty: an issue with no
// message still has a rule, and always has a key.
func title(issue map[string]any) string {
	candidate := strings.TrimSpace(stringField(issue, "message"))
	if candidate == "" {
		candidate = stringField(issue, "rule")
	}
	if candidate == "" {
		candidate = stringField(issue, "key")
	}
	return truncateRunes(candidate, titleLimit)
}

// description says where the finding is, in one line.
func description(issue map[string]any, path string) string {
	rule := stringField(issue, "rule")
	if path == "" {
		if rule != "" {
			return rule
		}
		if component := stringField(issue, "component"); component != "" {
			return component
		}
		return stringField(issue, "key")
	}

	location := path
	if line, ok := intField(issue, "line"); ok {
		location += ":" + strconv.FormatInt(line, 10)
	}
	if rule == "" {
		return location
	}
	return rule + " — " + location
}

// labels returns the queryable facts about a finding that never change for
// it, reduced to what the Catalog accepts.
//
// Only immutable facts belong here: the Catalog ingestion route stores an
// item metadata when the item is created and ignores it on every update, so a
// status or a severity label would keep its first value forever. Those live
// in the spec, which is replaced on every analysis and is selectable.
func labels(issue map[string]any, analysis analysisContext) map[string]any {
	result := make(map[string]any)
	setLabel(result, "project", analysis.projectKey)
	setLabel(result, "branch", analysis.branch)
	setLabel(result, "pull-request", analysis.pullRequest)
	setLabel(result, "type", stringField(issue, "type"))
	return result
}

// setLabel sets a prefixed label, dropping a value that sanitises to nothing:
// an empty label matches every query for it.
func setLabel(labels map[string]any, key, value string) {
	if sanitised := sanitiseLabelValue(value); sanitised != "" {
		labels[labelPrefix+key] = sanitised
	}
}

// tags returns this source's own tag and the issue tags. The severity is left
// out for the same reason as in labels: it can change, and tags cannot.
func tags(issue map[string]any) []any {
	result := []any{baseTag}
	seen := map[string]struct{}{baseTag: {}}
	add := func(raw string) {
		tag := sanitiseTag(raw)
		if tag == "" {
			return
		}
		if _, duplicate := seen[tag]; duplicate {
			return
		}
		seen[tag] = struct{}{}
		result = append(result, tag)
	}

	issueTags, _ := issue["tags"].([]any)
	for _, raw := range issueTags {
		if tag, ok := raw.(string); ok {
			add(tag)
		}
	}

	return result
}

// sonarqubeIssueURL is a link to the finding in the SonarQube issues view, or
// "" when the server URL is not one a browser should follow. The server URL may
// come from the webhook body, and anything but http(s) (javascript: included)
// must not end up on an item.
func sonarqubeIssueURL(issue map[string]any, analysis analysisContext) string {
	base, err := url.Parse(analysis.serverURL)
	if err != nil || !isHTTPURL(base) {
		return ""
	}

	issueKey := stringField(issue, "key")
	query := url.Values{}
	query.Set("id", analysis.projectKey)
	query.Set("issues", issueKey)
	query.Set("open", issueKey)
	if analysis.branch != "" {
		query.Set("branch", analysis.branch)
	}
	if analysis.pullRequest != "" {
		query.Set("pullRequest", analysis.pullRequest)
	}

	link := base.JoinPath("project", "issues")
	link.RawQuery = query.Encode()
	link.Fragment = ""
	return link.String()
}

// projectSlug reduces a project key to the readable prefix of an item name.
func projectSlug(projectKey string) string {
	if slug := sanitiseName(projectKey, projectSlugLimit); slug != "" {
		return slug
	}
	return projectSlugFallback
}

// sanitiseName reduces raw to lowercase ASCII alphanumerics separated by
// single dashes, at most limit characters long. It must stay byte-for-byte
// identical to the naming of catalog-sonarqube-webhook, so that items written
// by it are updated in place rather than duplicated.
func sanitiseName(raw string, limit int) string {
	var builder strings.Builder
	lastWasSeparator := true
	for i := 0; i < len(raw) && builder.Len() < limit; i++ {
		b := asciiLower(raw[i])
		switch {
		case isASCIIAlphanumeric(b):
			builder.WriteByte(b)
			lastWasSeparator = false
		case !lastWasSeparator:
			builder.WriteByte('-')
			lastWasSeparator = true
		}
	}
	return strings.Trim(builder.String(), "-")
}

// sanitiseLabelValue reduces raw to the Catalog label value pattern
// ^([a-zA-Z0-9]([a-zA-Z0-9._-]{0,61}[a-zA-Z0-9])?|)$.
func sanitiseLabelValue(raw string) string {
	var builder strings.Builder
	for _, r := range raw {
		if builder.Len() >= labelValueLimit {
			break
		}
		if isASCIIAlphanumeric(r) || r == '.' || r == '_' || r == '-' {
			builder.WriteRune(r)
		} else {
			builder.WriteByte('-')
		}
	}
	return strings.TrimFunc(builder.String(), func(r rune) bool {
		return !isASCIIAlphanumeric(r)
	})
}

// sanitiseTag reduces raw to the Catalog tag pattern ^[a-z0-9:+#]+(-[a-z0-9:+#]+)*$.
func sanitiseTag(raw string) string {
	var builder strings.Builder
	lastWasSeparator := true
	for _, r := range raw {
		if builder.Len() >= labelValueLimit {
			break
		}
		if l := asciiLower(r); isASCIIAlphanumeric(l) || l == ':' || l == '+' || l == '#' {
			builder.WriteRune(l)
			lastWasSeparator = false
			continue
		}
		if !lastWasSeparator {
			builder.WriteByte('-')
			lastWasSeparator = true
		}
	}
	return strings.Trim(builder.String(), "-")
}

// rfc3339 normalises a SonarQube timestamp to RFC 3339 in UTC, or returns ""
// when it cannot be parsed: a field declared date-time holding something else
// is worse than an absent one.
func rfc3339(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	for _, layout := range sonarqubeTimeLayouts {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed.UTC().Format(time.RFC3339)
		}
	}
	return ""
}

// parseTime parses a SonarQube timestamp, reporting whether it could.
func parseTime(raw string) (time.Time, bool) {
	normalised := rfc3339(raw)
	if normalised == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339, normalised)
	return parsed, err == nil
}

// optional maps an empty string to nil, so it renders as null in a template.
func optional(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// stringField returns object[key] when it is a string.
func stringField(object map[string]any, key string) string {
	value, _ := object[key].(string)
	return value
}

// intField returns object[key] when it is a JSON number.
func intField(object map[string]any, key string) (int64, bool) {
	value, ok := object[key].(float64)
	if !ok {
		return 0, false
	}
	return int64(value), true
}

// truncateRunes cuts s to limit characters without splitting one.
func truncateRunes(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit])
}

// asciiLower lowercases an ASCII letter and leaves every other character alone.
func asciiLower[T byte | rune](c T) T {
	if c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}
