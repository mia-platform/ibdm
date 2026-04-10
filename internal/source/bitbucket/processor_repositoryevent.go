// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package bitbucket

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/mia-platform/ibdm/internal/source"
)

// repositoryEventProcessor handles webhook events that affect repositories.
// It is shared by repo:push, repo:updated, and pullrequest:fulfilled events.
type repositoryEventProcessor struct{}

// process extracts the repository from the webhook payload, enriches it via a
// GET request to the API, and returns a single repository upsert event.
func (p *repositoryEventProcessor) process(ctx context.Context, c *client, typesToStream map[string]source.Extra, body []byte) ([]source.Data, error) {
	// Guard: caller didn't request repository type → skip
	if _, ok := typesToStream[repositoryType]; !ok {
		return nil, nil
	}

	// Parse webhook body
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("failed to parse webhook body: %w", err)
	}

	// Extract repository object from the payload
	repoRaw, ok := payload["repository"]
	if !ok {
		return nil, errors.New("webhook payload missing 'repository' field")
	}
	repo, ok := repoRaw.(map[string]any)
	if !ok {
		return nil, errors.New("webhook payload 'repository' field is not an object")
	}

	// Extract workspace and repo slug for enrichment
	fullName, _ := repo["full_name"].(string)
	workspaceSlug, repoSlug := splitFullName(fullName)
	if workspaceSlug == "" || repoSlug == "" {
		return nil, fmt.Errorf("unable to extract workspace/repo slug from full_name: %q", fullName)
	}

	// Enrich: fetch full repository details from the API
	fullRepo, err := c.getRepository(ctx, workspaceSlug, repoSlug)
	if err != nil {
		// Fall back to the webhook payload if the API call fails
		fullRepo = repo
	}

	return []source.Data{
		{
			Type:      repositoryType,
			Operation: source.DataOperationUpsert,
			Values: map[string]any{
				"repository": fullRepo,
			},
			Time: updatedOnOrNow(fullRepo),
		},
	}, nil
}
