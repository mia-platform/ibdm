// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package azuredevops

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/mia-platform/ibdm/internal/source"
)

const (
	gitRepositoryType = "gitrepository"
	teamType          = "team"

	continuationHeader = "X-MS-ContinuationToken"
)

var timeSource = time.Now

func syncResources(ctx context.Context, client *client, typesToFilter map[string]source.Extra, dataChannel chan<- source.Data) (err error) {
	for typeString := range typesToFilter {
		var path string
		queryParam := url.Values{}
		switch typeString {
		case gitRepositoryType:
			path = "_apis/git/repositories"
			queryParam.Set("includeLinks", "true")
			queryParam.Set("includeAllUrls", "true")
			queryParam.Set("includeHidden", "true")
		case teamType:
			path = "_apis/teams"
			queryParam.Set("$expandIdentity", "true")
		}

		for {
			resp, err := client.doRequest(ctx, http.MethodGet, path, queryParam)
			if err != nil {
				return err
			}
			defer resp.Body.Close()

			results, err := unmarshalResponse(resp.Body)
			if err != nil {
				return err
			}

			for _, item := range results {
				dataChannel <- source.Data{
					Type:      typeString,
					Operation: source.DataOperationUpsert,
					Time:      timeSource(),
					Values:    item,
				}
			}

			if nextLink := resp.Header.Get(continuationHeader); nextLink != "" { //nolint:canonicalheader
				queryParam.Set("continuationToken", nextLink)
				continue
			}

			break
		}
	}

	return err
}

func unmarshalResponse(body io.Reader) ([]map[string]any, error) {
	type resultsStruct struct {
		Count int              `json:"count"`
		Value []map[string]any `json:"value"`
	}

	results := new(resultsStruct)
	unmarshaler := json.NewDecoder(body)
	err := unmarshaler.Decode(&results)
	if err != nil {
		return nil, err
	}

	return results.Value, nil
}
