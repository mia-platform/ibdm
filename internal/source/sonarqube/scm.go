// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package sonarqube

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// provider identifies the forge a repository is hosted on, which decides the
// shape of the link to a line of code.
type provider int

const (
	// providerAuto infers the forge from the repository host.
	providerAuto provider = iota
	providerGitLab
	providerGitHub
	providerBitbucket
	providerBitbucketServer
	providerAzureDevOps
	// providerCustom uses SONARQUBE_SCM_URL_TEMPLATE only.
	providerCustom
	// providerNone disables SCM links.
	providerNone
)

// repositoryURLProperty is the analysis property a CI job passes the
// repository URL in.
const repositoryURLProperty = "sonar.analysis.repoUrl"

// scmTemplates holds how a forge spells a link to a file and to a line in it.
type scmTemplates struct {
	// file renders {repo}, {ref} and {path}.
	file string
	// line is appended to the file URL and renders {line}.
	line string
}

var (
	gitlabTemplates          = scmTemplates{file: "{repo}/-/blob/{ref}/{path}", line: "#L{line}"}
	githubTemplates          = scmTemplates{file: "{repo}/blob/{ref}/{path}", line: "#L{line}"}
	bitbucketTemplates       = scmTemplates{file: "{repo}/src/{ref}/{path}", line: "#lines-{line}"}
	bitbucketServerTemplates = scmTemplates{file: "{repo}/browse/{path}?at={ref}", line: "#{line}"}
	// azureDevOpsTemplates uses the GC prefix, which marks the ref as a commit:
	// a branch fallback cannot work here.
	azureDevOpsTemplates = scmTemplates{file: "{repo}?path=/{path}&version=GC{ref}", line: "&line={line}"}
)

// parseProvider parses SONARQUBE_SCM_PROVIDER. An unknown value is an error
// rather than a silent fallback: a link built for the wrong forge 404s on
// every finding.
func parseProvider(raw string) (provider, error) {
	switch strings.ReplaceAll(strings.ToLower(strings.TrimSpace(raw)), "_", "-") {
	case "", "auto":
		return providerAuto, nil
	case "gitlab":
		return providerGitLab, nil
	case "github":
		return providerGitHub, nil
	case "bitbucket", "bitbucket-cloud":
		return providerBitbucket, nil
	case "bitbucket-server", "bitbucket-datacenter":
		return providerBitbucketServer, nil
	case "azure-devops", "azuredevops", "ado":
		return providerAzureDevOps, nil
	case "custom":
		return providerCustom, nil
	case "none", "off":
		return providerNone, nil
	default:
		return providerAuto, fmt.Errorf("%w: SONARQUBE_SCM_PROVIDER %q is not one of auto, gitlab, github, bitbucket, bitbucket-server, azure-devops, custom, none", ErrInvalidEnvVariable, raw)
	}
}

// templates returns the built-in templates of p, resolving providerAuto
// against the repository host.
func (p provider) templates(repositoryURL string) (scmTemplates, bool) {
	switch p {
	case providerGitLab:
		return gitlabTemplates, true
	case providerGitHub:
		return githubTemplates, true
	case providerBitbucket:
		return bitbucketTemplates, true
	case providerBitbucketServer:
		return bitbucketServerTemplates, true
	case providerAzureDevOps:
		return azureDevOpsTemplates, true
	case providerAuto:
		return detectProvider(repositoryURL).templates(repositoryURL)
	case providerCustom, providerNone:
		return scmTemplates{}, false
	}
	return scmTemplates{}, false
}

// needsRevision reports whether the forge can only address a commit, so that
// a branch fallback would produce a broken link.
func (p provider) needsRevision(repositoryURL string) bool {
	return p == providerAzureDevOps || p == providerAuto && detectProvider(repositoryURL) == providerAzureDevOps
}

// detectProvider guesses the forge from the repository host. Only the hosted
// services are recognisable: a self-hosted GitLab or Bitbucket Data Center is
// on a host of the customer's choosing and must be named explicitly. An
// unrecognised host yields providerNone.
func detectProvider(repositoryURL string) provider {
	parsed, err := url.Parse(repositoryURL)
	if err != nil {
		return providerNone
	}
	host := strings.ToLower(parsed.Hostname())

	switch {
	case host == "gitlab.com" || strings.HasSuffix(host, ".gitlab.com"):
		return providerGitLab
	case host == "github.com" || strings.HasSuffix(host, ".github.com"):
		return providerGitHub
	case host == "bitbucket.org" || strings.HasSuffix(host, ".bitbucket.org"):
		return providerBitbucket
	case host == "dev.azure.com" || strings.HasSuffix(host, ".visualstudio.com"):
		return providerAzureDevOps
	default:
		return providerNone
	}
}

// scmSettings groups everything deciding the SCM link of an item.
type scmSettings struct {
	provider    provider
	urlTemplate string
}

// splitURLTemplate splits a custom URL template into its file and line parts.
// The line part starts at the last '?', '&' or '#' before {line}, so that
// "{repo}/files/{ref}/{path}?highlight={line}" links a file on its own when
// the issue carries no line.
func splitURLTemplate(template string) scmTemplates {
	index := strings.Index(template, "{line}")
	if index < 0 {
		return scmTemplates{file: template}
	}
	start := strings.LastIndexAny(template[:index], "?&#")
	if start < 0 {
		start = index
	}
	return scmTemplates{file: template[:start], line: template[start:]}
}

// scmTarget is a repository and the reference an analysis saw it at, ready to
// render the URL of any file in it.
type scmTarget struct {
	// repositoryURL is the repository root, without trailing slash or .git suffix.
	repositoryURL string
	// reference is the analysed commit, or the branch as a fallback.
	reference    string
	fileTemplate string
	lineTemplate string
}

// newSCMTarget builds a target, or returns nil with the reason when there is
// nothing to link to: no repository URL, no revision and no branch, provider
// none, an unrecognisable host under auto, custom without a template, or an
// Azure DevOps repository without a revision.
//
// The revision is preferred over the branch: a finding is about the code as
// the analysis saw it, and a link to a branch points at whatever is on it now.
func newSCMTarget(repositoryURL, revision, branch string, settings scmSettings) (*scmTarget, string) {
	if settings.provider == providerNone {
		return nil, ""
	}

	normalised, ok := normaliseRepositoryURL(repositoryURL)
	if !ok {
		return nil, ""
	}

	var templates scmTemplates
	switch {
	case settings.provider == providerCustom || settings.urlTemplate != "":
		if settings.urlTemplate == "" {
			return nil, "SONARQUBE_SCM_PROVIDER is 'custom' but SONARQUBE_SCM_URL_TEMPLATE is empty; no SCM link is written"
		}
		templates = splitURLTemplate(settings.urlTemplate)
	default:
		builtIn, found := settings.provider.templates(normalised)
		if !found {
			return nil, fmt.Sprintf("cannot tell which forge %q is on; set SONARQUBE_SCM_PROVIDER to write SCM links", normalised)
		}
		templates = builtIn
	}

	var reference string
	switch {
	case strings.TrimSpace(revision) != "":
		reference = strings.TrimSpace(revision)
	case strings.TrimSpace(branch) != "":
		if settings.provider.needsRevision(normalised) {
			return nil, "the analysis carries no revision, and an Azure DevOps link can only address a commit; no SCM link is written"
		}
		reference = strings.TrimSpace(branch)
	default:
		return nil, ""
	}

	return &scmTarget{
		repositoryURL: normalised,
		reference:     reference,
		fileTemplate:  templates.file,
		lineTemplate:  templates.line,
	}, ""
}

// url returns the URL of path, anchored at line when line is positive.
func (t *scmTarget) url(path string, line int64) (string, bool) {
	path = strings.TrimLeft(path, "/")
	if path == "" {
		return "", false
	}

	rendered := strings.NewReplacer(
		"{repo}", t.repositoryURL,
		"{ref}", encodeComponent(t.reference),
		"{path}", encodePath(path),
	).Replace(t.fileTemplate)

	if line > 0 && t.lineTemplate != "" {
		rendered += strings.ReplaceAll(t.lineTemplate, "{line}", strconv.FormatInt(line, 10))
	}

	return rendered, true
}

// normaliseRepositoryURL reduces what was configured, or read out of
// SonarQube, to a plain browsable repository root. Three shapes turn up that a
// browser cannot open: Maven's scm:git:https://…, the SSH form
// git@host:group/repo.git, and a URL with a .git suffix.
func normaliseRepositoryURL(raw string) (string, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", false
	}

	for _, prefix := range []string{"scm:git:", "scm:svn:", "scm:"} {
		if stripped, found := strings.CutPrefix(value, prefix); found {
			value = strings.TrimSpace(stripped)
			break
		}
	}

	if rest, found := strings.CutPrefix(value, "git@"); found {
		if host, path, hasPath := strings.Cut(rest, ":"); hasPath {
			value = "https://" + host + "/" + path
		}
	}

	value = strings.TrimRight(value, "/")
	value = strings.TrimSuffix(value, ".git")

	parsed, err := url.Parse(value)
	if err != nil || !isHTTPURL(parsed) {
		// ssh://, file:// and the rest are not links a person can follow.
		return "", false
	}

	return value, true
}

// encodeComponent percent-encodes one URL component, leaving only ASCII
// alphanumerics and "-._~" unescaped. A branch like release/2.0 is one
// component, so its slash is escaped.
func encodeComponent(value string) string {
	var builder strings.Builder
	builder.Grow(len(value))
	for i := range len(value) {
		b := value[i]
		if isASCIIAlphanumeric(b) || b == '-' || b == '.' || b == '_' || b == '~' {
			builder.WriteByte(b)
			continue
		}
		fmt.Fprintf(&builder, "%%%02X", b)
	}
	return builder.String()
}

// encodePath percent-encodes a path, keeping its separators.
func encodePath(path string) string {
	segments := strings.Split(path, "/")
	for i, segment := range segments {
		segments[i] = encodeComponent(segment)
	}
	return strings.Join(segments, "/")
}

// isHTTPURL reports whether u is an absolute http(s) URL: the only kind a
// person can follow, and the only kind written onto an item.
func isHTTPURL(u *url.URL) bool {
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// isASCIIAlphanumeric reports whether c is an ASCII letter or digit.
func isASCIIAlphanumeric[T byte | rune](c T) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}
