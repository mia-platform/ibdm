// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package jenkins

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/mia-platform/ibdm/internal/source"
)

const (
	systemStorePath   = "/credentials/store/system"
	folderStoreSuffix = "/credentials/store/folder"

	storeKindSystem = "system"
	storeKindFolder = "folder"

	// credentialTree selects only the metadata of the credentials and the names of the jobs
	// using them: secret values and fingerprint hashes are never requested.
	credentialTree = "domains[urlName,credentials[id,displayName,typeName,description,fingerprint[usage[name]]]]"
)

// credentialStore is the response describing a credentials store and its domains.
type credentialStore struct {
	Domains json.RawMessage `json:"domains"`
}

// credentialDomain is a domain of a credentials store with its credentials.
type credentialDomain struct {
	URLName     string            `json:"urlName"`
	Credentials []credentialEntry `json:"credentials"`
}

// credentialEntry holds the metadata of a credential.
type credentialEntry struct {
	ID          string                 `json:"id"`
	DisplayName string                 `json:"displayName"`
	TypeName    string                 `json:"typeName"`
	Description string                 `json:"description"`
	Fingerprint *credentialFingerprint `json:"fingerprint"`
}

// credentialFingerprint records the jobs that used a credential.
type credentialFingerprint struct {
	Usage []fingerprintUsage `json:"usage"`
}

// fingerprintUsage is a job that used a credential, identified by its full name.
type fingerprintUsage struct {
	Name string `json:"name"`
}

// storeRef identifies a credentials store: the system store or the store of a container.
type storeRef struct {
	kind   string
	path   string
	folder *container
}

// syncCredentials emits the credentials of the system store and of the store of every folder and
// multibranch project. A failure reading the system store is returned; failures on folder stores
// are logged and the sync continues.
func (s *Source) syncCredentials(ctx context.Context, inv *inventory, results chan<- source.Data) error {
	log := logger.FromContext(ctx).WithName(loggerName)

	if err := s.syncStore(ctx, inv, storeRef{kind: storeKindSystem, path: systemStorePath}, results); err != nil {
		return fmt.Errorf("system credentials store: %w", err)
	}

	for idx := range inv.containers {
		folder := &inv.containers[idx]
		store := storeRef{kind: storeKindFolder, path: jobPath(folder.segments) + folderStoreSuffix, folder: folder}
		err := s.syncStore(ctx, inv, store, results)
		var statusErr *statusError
		switch {
		case err == nil:
		case ctx.Err() != nil:
			return ctx.Err()
		case errors.As(err, &statusErr) && statusErr.StatusCode == http.StatusNotFound:
			log.Debug("no credentials store for folder", "fullName", folder.fullName)
		default:
			log.Error("failed to read folder credentials store, skipping", "fullName", folder.fullName, "error", err.Error())
		}
	}

	return nil
}

// syncStore emits every credential of a store.
func (s *Source) syncStore(ctx context.Context, inv *inventory, store storeRef, results chan<- source.Data) error {
	var response credentialStore
	if _, err := s.client.getJSON(ctx, store.path+apiJSONPath, credentialTree, &response); err != nil {
		return err
	}

	domains, err := decodeDomains(response.Domains)
	if err != nil {
		return fmt.Errorf("failed to decode credential domains of %s: %w", store.path, err)
	}

	for _, domain := range domains {
		for _, credential := range domain.Credentials {
			if err := send(ctx, results, credentialType, credentialValues(ctx, inv, store, domain.URLName, credential)); err != nil {
				return err
			}
		}
	}

	return nil
}

// decodeDomains decodes the domains of a credentials store, which Jenkins exposes as an object
// keyed by domain name; an array of domains is accepted too. Domains are returned ordered by name.
func decodeDomains(raw json.RawMessage) ([]credentialDomain, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}

	if trimmed[0] == '[' {
		var domains []credentialDomain
		if err := json.Unmarshal(trimmed, &domains); err != nil {
			return nil, err
		}
		return domains, nil
	}

	var byName map[string]credentialDomain
	if err := json.Unmarshal(trimmed, &byName); err != nil {
		return nil, err
	}

	domains := make([]credentialDomain, 0, len(byName))
	for _, name := range slices.Sorted(maps.Keys(byName)) {
		domain := byName[name]
		if domain.URLName == "" {
			domain.URLName = name
		}
		domains = append(domains, domain)
	}
	return domains, nil
}

// credentialValues returns the values of a credential with its store and its usages.
func credentialValues(ctx context.Context, inv *inventory, store storeRef, domain string, credential credentialEntry) map[string]any {
	storeValues := map[string]any{
		keyKind:  store.kind,
		"domain": domain,
	}
	if store.folder != nil {
		storeValues["folder"] = store.folder.values()
	}

	var usages []fingerprintUsage
	if credential.Fingerprint != nil {
		usages = credential.Fingerprint.Usage
	}

	return map[string]any{
		"credential": map[string]any{
			"id":           credential.ID,
			"displayName":  credential.DisplayName,
			"typeName":     credential.TypeName,
			keyDescription: credential.Description,
		},
		"store":  storeValues,
		"usages": resolveUsages(ctx, inv, credential.ID, usages),
	}
}

// resolveUsages maps the jobs that used a credential to the workflows and multibranch projects
// known to the sync. A branch job is reported as its multibranch project, once, listing the
// branches; jobs that are neither, such as freestyle or deleted jobs, are discarded.
func resolveUsages(ctx context.Context, inv *inventory, credentialID string, usages []fingerprintUsage) []map[string]any {
	log := logger.FromContext(ctx).WithName(loggerName)

	out := make([]map[string]any, 0, len(usages))
	projectUsages := make(map[string]map[string]any)
	projectBranches := make(map[string][]string)
	for _, usage := range usages {
		if entry, ok := inv.index[usage.Name]; ok && entry.kind == usageKindWorkflow {
			out = append(out, usageValues(entry))
			continue
		}

		parentName, branch, found := cutLast(usage.Name)
		entry, ok := inv.index[parentName]
		if !found || !ok || entry.kind != usageKindMultibranch {
			log.Debug("discarding credential usage by an unsupported or unknown job", "credential", credentialID, "job", usage.Name)
			continue
		}

		if _, seen := projectUsages[entry.fullName]; !seen {
			values := usageValues(entry)
			projectUsages[entry.fullName] = values
			out = append(out, values)
		}
		projectBranches[entry.fullName] = append(projectBranches[entry.fullName], branch)
	}

	for fullName, values := range projectUsages {
		values["branches"] = projectBranches[fullName]
	}

	return out
}

// usageValues returns a credential usage by a workflow or a multibranch project, with its parent.
func usageValues(entry indexEntry) map[string]any {
	return map[string]any{
		keyKind:     entry.kind,
		keyFullName: entry.fullName,
		keyURL:      entry.url,
		"parent":    entry.parent.values(),
	}
}

// cutLast splits a full name around its last slash, returning the parent full name and the
// name of the item.
func cutLast(fullName string) (string, string, bool) {
	idx := strings.LastIndex(fullName, "/")
	if idx < 0 {
		return "", "", false
	}
	return fullName[:idx], fullName[idx+1:], true
}
