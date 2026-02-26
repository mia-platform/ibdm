// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package sysdig

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/mia-platform/ibdm/internal/logger"
)

const (
	sysqlAPIPath = "/api/sysql/v2/query"
)

// sysqlRequest is the JSON body sent to the SysQL API.
type sysqlRequest struct {
	Query string `json:"query"`
}

// sysqlResponse represents the JSON response from the SysQL API.
type sysqlResponse struct {
	Data    sysqlData    `json:"data"`
	Summary sysqlSummary `json:"summary"`
}

// sysqlData holds the items array from a SysQL response.
type sysqlData struct {
	Items []map[string]any `json:"items"`
}

// sysqlSummary contains metadata about a SysQL query result.
type sysqlSummary struct {
	FetchedItemsCount int `json:"fetched_items_count"` //nolint:tagliatelle // Sysdig API uses snake_case
}

// queryAllPages executes the given SysQL query with LIMIT/OFFSET pagination,
// calling handlePage for each batch of items. It stops when a page returns
// zero items or the context is cancelled.
func queryAllPages(ctx context.Context, httpClient *http.Client, baseURL, apiToken, query string, pageSize int, handlePage func([]map[string]any) error) error {
	log := logger.FromContext(ctx).WithName(loggerName)

	offset := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		paginatedQuery := fmt.Sprintf("%s LIMIT %d OFFSET %d;", query, pageSize, offset)
		log.Trace("executing SysQL query", "query", paginatedQuery, "offset", offset)

		resp, err := executeSysQLQuery(ctx, httpClient, baseURL, apiToken, paginatedQuery)
		if err != nil {
			return err
		}

		log.Trace("SysQL page fetched", "fetchedItems", resp.Summary.FetchedItemsCount, "offset", offset)

		if resp.Summary.FetchedItemsCount == 0 {
			return nil
		}

		if err := handlePage(resp.Data.Items); err != nil {
			return err
		}

		offset += resp.Summary.FetchedItemsCount
	}
}

// executeSysQLQuery sends a single SysQL query to the Sysdig API and decodes
// the response. It returns an error for non-2xx status codes, network failures,
// and JSON decode errors.
func executeSysQLQuery(ctx context.Context, httpClient *http.Client, baseURL, apiToken, query string) (*sysqlResponse, error) {
	reqBody := sysqlRequest{Query: query}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling SysQL request: %w", err)
	}

	url := baseURL + sysqlAPIPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("creating SysQL request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiToken)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing SysQL request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("SysQL API returned status %d: %s", resp.StatusCode, string(body))
	}

	var sysqlResp sysqlResponse
	if err := json.NewDecoder(resp.Body).Decode(&sysqlResp); err != nil {
		return nil, fmt.Errorf("decoding SysQL response: %w", err)
	}

	return &sysqlResp, nil
}
