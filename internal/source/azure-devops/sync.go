// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package azuredevops

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"

	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/mia-platform/ibdm/internal/source"
)

const (
	gitRepositoryType = "gitrepository"
)

var timeSource = time.Now

func syncResources(ctx context.Context, connection *azuredevops.Connection, typesToFilter map[string]source.Extra, dataChannel chan<- source.Data) (err error) {
	for typeString := range typesToFilter {
		switch typeString {
		case gitRepositoryType:
			err = syncGitRepositories(ctx, connection, dataChannel)
		}
	}

	return err
}

func syncGitRepositories(ctx context.Context, connection *azuredevops.Connection, dataChannel chan<- source.Data) error {
	log := logger.FromContext(ctx).WithName(logName)
	client, err := git.NewClient(ctx, connection)
	if err != nil {
		return err
	}

	timestamp := timeSource()
	response, err := client.GetRepositories(ctx, git.GetRepositoriesArgs{
		IncludeLinks:   to.Ptr(true),
		IncludeAllUrls: to.Ptr(true),
		IncludeHidden:  to.Ptr(true),
	})
	if err != nil {
		return err
	}

	for _, repo := range *response {
		values, err := valuesFromObject(repo)
		if err != nil {
			log.Error("fail to parse git repository", "error", err.Error(), "repositoryId", *repo.Id)
			continue
		}

		dataChannel <- source.Data{
			Type:      gitRepositoryType,
			Operation: source.DataOperationUpsert,
			Time:      timestamp,
			Values:    values,
		}
	}

	return nil
}

func valuesFromObject(obj any) (map[string]any, error) {
	data, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}

	values := map[string]any{}
	err = json.Unmarshal(data, &values)
	return values, err
}
