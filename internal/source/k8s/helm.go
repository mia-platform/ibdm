// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package k8s

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/mia-platform/ibdm/internal/source"
)

const (
	// helmSecretSelector selects the Secrets that store the current revision of every Helm release.
	helmSecretSelector = "owner=helm,status=deployed" //nolint:gosec // label selector, not a credential

	// helmReleaseKey is the Secret data key holding the encoded Helm release.
	helmReleaseKey = "release"

	// helmNameLabel and helmVersionLabel are the bookkeeping labels Helm puts on its release Secrets.
	helmNameLabel    = "name"
	helmVersionLabel = "version"

	// maxDecodedReleaseSize bounds the size of a decompressed release to protect against compression bombs.
	maxDecodedReleaseSize = 256 << 20
)

// gzipMagic is the prefix of every gzip stream.
var gzipMagic = []byte{0x1f, 0x8b}

// helmRelease is the minimal slice of Helm's release object that is decoded.
// The manifest, the values and the chart files are deliberately not captured.
type helmRelease struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Version   int    `json:"version"`
	Info      struct {
		Status        string     `json:"status"`
		FirstDeployed flexString `json:"first_deployed"` //nolint:tagliatelle // Helm storage format
		LastDeployed  flexString `json:"last_deployed"`  //nolint:tagliatelle // Helm storage format
	} `json:"info"`
	Chart struct {
		Metadata struct {
			Name       string `json:"name"`
			Version    string `json:"version"`
			AppVersion string `json:"appVersion"`
		} `json:"metadata"`
	} `json:"chart"`
}

// flexString is a string that tolerates any other JSON value, which it turns into an empty string.
type flexString string

// UnmarshalJSON keeps JSON strings and ignores every other JSON value.
func (f *flexString) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		*f = ""
		return nil //nolint:nilerr // non-string values are intentionally ignored
	}
	*f = flexString(value)
	return nil
}

// decodeHelmRelease decodes the content of a Helm release Secret: base64, then
// gzip when the gzip magic bytes are present, then JSON.
func decodeHelmRelease(data []byte) (*helmRelease, error) {
	raw := make([]byte, base64.StdEncoding.DecodedLen(len(data)))
	n, err := base64.StdEncoding.Decode(raw, data)
	if err != nil {
		return nil, fmt.Errorf("decoding base64: %w", err)
	}
	raw = raw[:n]

	if bytes.HasPrefix(raw, gzipMagic) {
		reader, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, fmt.Errorf("opening gzip stream: %w", err)
		}
		defer reader.Close()

		if raw, err = io.ReadAll(io.LimitReader(reader, maxDecodedReleaseSize)); err != nil {
			return nil, fmt.Errorf("reading gzip stream: %w", err)
		}
	}

	var release helmRelease
	if err := json.Unmarshal(raw, &release); err != nil {
		return nil, fmt.Errorf("decoding json: %w", err)
	}
	return &release, nil
}

// syncHelmReleases emits one helmrelease item per deployed Helm release of the
// cluster, read from Helm's release Secrets. Secrets that cannot be decoded are
// logged (name and namespace only) and skipped.
func (s *Source) syncHelmReleases(ctx context.Context, results chan<- source.Data) error {
	log := logger.FromContext(ctx).WithName(loggerName)
	releases := make(map[string]map[string]any)

	err := listPages(ctx, "helm release secrets",
		func(ctx context.Context, options metav1.ListOptions) ([]corev1.Secret, string, error) {
			options.LabelSelector = helmSecretSelector
			list, err := s.clientset.CoreV1().Secrets(metav1.NamespaceAll).List(ctx, options)
			if err != nil {
				return nil, "", err
			}
			return list.Items, list.Continue, nil
		},
		func(secret *corev1.Secret) error {
			values, err := helmSecretValues(secret, s.apiServer)
			if err != nil {
				log.Warn("error decoding helm release secret, skipping", "secret", secret.Name, "namespace", secret.Namespace, "error", err.Error())
				return nil
			}

			key := values[keyNamespace].(string) + "/" + values[keyName].(string)                          //nolint:forcetypeassert // always strings
			if current, ok := releases[key]; ok && current["revision"].(int) >= values["revision"].(int) { //nolint:forcetypeassert // always int
				return nil
			}
			releases[key] = values
			return nil
		},
	)
	if err != nil {
		return err
	}

	keys := make([]string, 0, len(releases))
	for key := range releases {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		if err := send(ctx, results, s.workloadData(helmReleaseType, releases[key])); err != nil {
			return err
		}
	}
	return nil
}

// helmSecretValues decodes a Helm release Secret and builds the values of the
// matching helmrelease item.
func helmSecretValues(secret *corev1.Secret, apiServer string) (map[string]any, error) {
	release, err := decodeHelmRelease(secret.Data[helmReleaseKey])
	if err != nil {
		return nil, err
	}
	return helmReleaseValues(secret, release, apiServer), nil
}

// helmReleaseValues builds the values of a helmrelease item. Name, namespace
// and revision fall back to the Secret's metadata when the release lacks them.
func helmReleaseValues(secret *corev1.Secret, release *helmRelease, apiServer string) map[string]any {
	name := release.Name
	if name == "" {
		name = secret.Labels[helmNameLabel]
	}
	namespace := release.Namespace
	if namespace == "" {
		namespace = secret.Namespace
	}
	revision := release.Version
	if revision == 0 {
		if parsed, err := strconv.Atoi(secret.Labels[helmVersionLabel]); err == nil {
			revision = parsed
		}
	}

	return map[string]any{
		keyAPIServer:    apiServer,
		keyName:         name,
		keyNamespace:    namespace,
		"revision":      revision,
		"status":        release.Info.Status,
		"chartName":     release.Chart.Metadata.Name,
		"chartVersion":  release.Chart.Metadata.Version,
		"appVersion":    release.Chart.Metadata.AppVersion,
		"firstDeployed": string(release.Info.FirstDeployed),
		"lastDeployed":  string(release.Info.LastDeployed),
	}
}
