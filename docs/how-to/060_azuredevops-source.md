# Microsoft Azure Devops Integration

The Microsoft Azure Devops Integration of `ibdm` can work in two modes:

- receiving webhooks events
- getting resources via REST APIs

## Commands

Once you have the `ibdm` binary available the run of the integration is straightforward.

If you want to start a new integration with the EventHub subscription yuo can run the following
command:

```sh
ibdm run azure-devops --mapping-file <path to mapping file or folder>
```

if you want to start a resource graph sync process run this instead:

```sh
ibdm sync azure-devops --mapping-file <path to mapping file or folder>
```

## Configurations

In addition to other environment variables the Microsoft Azure Devops source require these additional ones:

- `AZURE_DEVOPS_ORGANIZATION_URL`: the Microsoft Azure Devops organization url
- `AZURE_DEVOPS_PERSONAL_TOKEN`: a personal access token of a user in the organization
- `AZURE_DEVOPS_WEBHOOK_PATH`: the path where the webhook handler will be exposed, by default will be `/azure-devops/webhook`
- `AZURE_DEVOPS_WEBHOOK_USER`: the optional user for webhook authorization
- `AZURE_DEVOPS_WEBHOOK_PASSWORD`: the optional password for webhook authorization

The first two variables are used if you start the integration in sync mode using the REST APIs.
The other three are needed if you start the source for exposing a webhook for receiving events from
Microsoft Azure DevOps.

If you set `AZURE_DEVOPS_WEBHOOK_USER` you need to also set `AZURE_DEVOPS_WEBHOOK_PASSWORD` and their
values must be the same set in the Azure DevOps dashboard during the webhook registration.

For now the source can access the following resources with the eventual webhook that can be registered for them:

- [repositories](https://learn.microsoft.com/en-us/rest/api/azure/devops/git/repositories/list?view=azure-devops-rest-7.1&tabs=HTTP#gitrepository)  
	webhook supported
	- [`git.repo.created`](https://learn.microsoft.com/en-us/azure/devops/service-hooks/events?view=azure-devops#repository-created)
	- [`git.repo.renamed`](https://learn.microsoft.com/en-us/azure/devops/service-hooks/events?view=azure-devops#repository-renamed)
	- [`git.repo.deleted`](https://learn.microsoft.com/en-us/azure/devops/service-hooks/events?view=azure-devops#repository-deleted)
- [teams](https://learn.microsoft.com/en-us/rest/api/azure/devops/core/teams/get-all-teams?view=azure-devops-rest-7.1&tabs=HTTP#webapiteam)

## Authentication

The source is using a personal access token for sending all the request to the Microsoft Azure
Devops REST API. The permission that the PAT must have is to read the resources that you intend
to access.
