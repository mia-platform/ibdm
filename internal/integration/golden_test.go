// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

//go:build integration

package integration

import (
	"bytes"
	"cmp"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	// goldenDir holds the golden files, one per scenario.
	goldenDir = "testdata/golden"

	// timePlaceholder replaces every operationTime: sources stamp items with the current time.
	timePlaceholder = "<time>"
	// catalogPlaceholder and upstreamPlaceholder replace the addresses of the fakes, which
	// change with every run.
	catalogPlaceholder  = "<catalog>"
	upstreamPlaceholder = "<upstream>"

	operationTimeKey = "operationTime"
	goldenFileMode   = 0o600
	goldenDirMode    = 0o750
)

// update rewrites the golden files instead of comparing with them.
var update = flag.Bool("update", false, "rewrite the golden files of the integration tests")

// golden is the content of a golden file: what reached the Catalog, and what was asked upstream.
type golden struct {
	CatalogItems     []map[string]any  `json:"catalogItems"`
	UpstreamRequests []recordedRequest `json:"upstreamRequests"`
}

// assertGolden compares the normalised traffic of catalog and upstream with the golden file
// name, or rewrites the file when the update flag is set. upstream may be nil. extraReplacements
// are old, new pairs applied to every string after the address placeholders: values that depend
// on the run, such as identifiers hashed from the address of a fake, become stable placeholders.
func assertGolden(t *testing.T, name string, catalog *fakeCatalog, upstream *fakeUpstream, extraReplacements ...string) {
	t.Helper()

	require.Zero(t, len(extraReplacements)%2, "extraReplacements must be old, new pairs")
	replacements := append([]string{}, extraReplacements...)
	replacements = append(replacements, hostOf(t, catalog.server.URL), catalogPlaceholder)
	var requests []recordedRequest
	if upstream != nil {
		replacements = append(replacements, hostOf(t, upstream.server.URL), upstreamPlaceholder)
		requests = upstream.received()
	}
	replacer := strings.NewReplacer(replacements...)

	actual := normalise(golden{CatalogItems: catalog.received(), UpstreamRequests: requests}, replacer)
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	// Placeholders hold angle brackets: escaping them would make the file hard to review.
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	require.NoError(t, encoder.Encode(actual))
	content := buffer.Bytes()

	path := filepath.Join(goldenDir, name+".json")
	if *update {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), goldenDirMode))
		require.NoError(t, os.WriteFile(path, content, goldenFileMode))
		return
	}

	expected, err := os.ReadFile(path)
	require.NoError(t, err, "missing golden file: run the test with -update and review the result")
	require.Equal(t, string(expected), string(content), "golden file %s differs: if the change is intended, run with -update and review the diff", path)
}

// hostOf returns the host and port of rawURL.
func hostOf(t *testing.T, rawURL string) string {
	t.Helper()

	parsed, err := url.Parse(rawURL)
	require.NoError(t, err)
	return parsed.Host
}

// normalise makes the traffic of a run comparable across runs: times and addresses become
// placeholders, and items and requests are sorted, since several sources emit in random order.
func normalise(traffic golden, replacer *strings.Replacer) golden {
	items := make([]map[string]any, 0, len(traffic.CatalogItems))
	for _, item := range traffic.CatalogItems {
		normalised, _ := replaceStrings(item, replacer).(map[string]any)
		if _, ok := normalised[operationTimeKey]; ok {
			normalised[operationTimeKey] = timePlaceholder
		}
		items = append(items, normalised)
	}
	slices.SortStableFunc(items, compareItems)

	requests := make([]recordedRequest, 0, len(traffic.UpstreamRequests))
	for _, request := range traffic.UpstreamRequests {
		request.Query = replacer.Replace(request.Query)
		if request.Header != nil {
			header := make(map[string]string, len(request.Header))
			for key, value := range request.Header {
				header[key] = replacer.Replace(value)
			}
			request.Header = header
		}
		requests = append(requests, request)
	}
	slices.SortStableFunc(requests, compareRequests)

	return golden{CatalogItems: items, UpstreamRequests: requests}
}

// replaceStrings returns a copy of value with replacer applied to every string inside it.
func replaceStrings(value any, replacer *strings.Replacer) any {
	switch typed := value.(type) {
	case string:
		return replacer.Replace(typed)
	case map[string]any:
		replaced := make(map[string]any, len(typed))
		for key, inner := range typed {
			replaced[key] = replaceStrings(inner, replacer)
		}
		return replaced
	case []any:
		replaced := make([]any, len(typed))
		for i, inner := range typed {
			replaced[i] = replaceStrings(inner, replacer)
		}
		return replaced
	default:
		return value
	}
}

// compareItems orders items by item type, name and operation, then by full content, so that
// the order never depends on arrival.
func compareItems(a, b map[string]any) int {
	for _, key := range []string{"apiVersion", "itemFamily", "name", "operation"} {
		if c := cmp.Compare(fmt.Sprint(a[key]), fmt.Sprint(b[key])); c != 0 {
			return c
		}
	}
	return cmp.Compare(canonical(a), canonical(b))
}

// compareRequests orders requests by method, path, query and recorded headers.
func compareRequests(a, b recordedRequest) int {
	return cmp.Or(
		cmp.Compare(a.Method, b.Method),
		cmp.Compare(a.Path, b.Path),
		cmp.Compare(a.Query, b.Query),
		cmp.Compare(canonical(a.Header), canonical(b.Header)),
	)
}

// canonical returns the JSON encoding of value, with map keys sorted.
func canonical(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(encoded)
}
