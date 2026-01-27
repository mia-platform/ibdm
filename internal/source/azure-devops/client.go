// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package azuredevops

import (
	"context"
	"net/http"
	"net/url"
)

type client struct {
	organizationURL *url.URL
	personalToken   string

	client http.Client
}

func newClient(organizationURL, personalToken string) (*client, error) {
	url, err := url.Parse(organizationURL)
	if err != nil {
		return nil, err
	}

	return &client{
		organizationURL: url,
		personalToken:   personalToken,
		client:          http.Client{},
	}, nil
}

func (c *client) doRequest(ctx context.Context, method string, path string, queryParam url.Values) (*http.Response, error) {
	url := c.organizationURL.JoinPath(path)
	url.RawQuery = queryParam.Encode()

	req, err := http.NewRequestWithContext(ctx, method, url.String(), nil)
	if err != nil {
		return nil, err
	}

	if c.personalToken != "" {
		req.SetBasicAuth("", c.personalToken)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json;api-version=7.1;charset=utf-8")
	return c.client.Do(req)
}
