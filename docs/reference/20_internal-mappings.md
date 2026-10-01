# Internal Mappings

These are the internal mappings shipped inside the `ibdm` binary, grouped by integration. Select them
with `--include-internal-mappings` and `--exclude-internal-mappings`: see
[Internal and External Mappings](../how-to/015_internal-and-external-mappings.md).
`ibdm mappings list <integration>` prints the names of one integration.

Every internal mapping is syncable. The root `extra` is the configuration the integration uses to
fetch the data: an external mapping on the same type that declares a different one makes the
integration fetch the data a second time.

The same mapping files are published with every release as `default-itds-mappings.tar.gz`, as a
starting point for your own external mappings.

## `azure`

| Name | Type | Item type (`apiVersion` `itemFamily`) | Root `extra` |
| --- | --- | --- | --- |
| `apim_services` | `Microsoft.ApiManagement/Service` | `azure.mia-platform.eu/v1` `apimservices` | `apiVersion`: `2024-05-01` |
| `cognitiveaccounts` | `Microsoft.CognitiveServices/accounts` | `azure.mia-platform.eu/v1` `cognitiveaccounts` | `apiVersion`: `2025-06-01` |
| `containerapps` | `Microsoft.App/containerApps` | `azure.mia-platform.eu/v1` `containerapps` | `apiVersion`: `2025-07-01` |
| `containerregistries` | `Microsoft.ContainerRegistry/registries` | `azure.mia-platform.eu/v1` `containerregistries` | `apiVersion`: `2025-11-01` |
| `keyvaults` | `Microsoft.KeyVault/vaults` | `azure.mia-platform.eu/v1` `keyvaults` | `apiVersion`: `2026-02-01` |
| `managedclusters` | `Microsoft.ContainerService/managedClusters` | `azure.mia-platform.eu/v1` `managedclusters` | `apiVersion`: `2025-10-01` |
| `notificationhubs_namespaces` | `Microsoft.NotificationHubs/namespaces` | `azure.mia-platform.eu/v1` `notificationhubsnamespaces` | `apiVersion`: `2023-09-01` |
| `notificationhubs_notificationhubs` | `Microsoft.NotificationHubs/namespaces/notificationhubs` | `azure.mia-platform.eu/v1` `notificationhubs` | `apiVersion`: `2017-04-01` |
| `postgresqldbs` | `Microsoft.DBforPostgreSQL/flexibleServers` | `azure.mia-platform.eu/v1` `postgresqldbs` | `apiVersion`: `2025-08-01` |
| `resourcegroups` | `Microsoft.Resources/resourceGroups` | `azure.mia-platform.eu/v1` `resourcegroups` | `apiVersion`: `2021-04-01` |
| `storageaccounts` | `Microsoft.Storage/storageAccounts` | `azure.mia-platform.eu/v1` `storageaccounts` | `apiVersion`: `2025-06-01` |
| `subscriptions` | `Microsoft.Resources/subscriptions` | `azure.mia-platform.eu/v1` `subscriptions` | `apiVersion`: `2022-12-01` |
| `virtualmachines` | `Microsoft.Compute/virtualMachines` | `azure.mia-platform.eu/v1` `virtualmachines` | `apiVersion`: `2025-04-01` |
| `virtualnetworks` | `Microsoft.Network/virtualNetworks` | `azure.mia-platform.eu/v1` `virtualnetworks` | `apiVersion`: `2025-03-01` |
| `websites` | `Microsoft.Web/sites` | `azure.mia-platform.eu/v1` `websites` | `apiVersion`: `2025-03-01` |
| `websites_functionapps` | `functionapps` | `azure.mia-platform.eu/v1` `functionapps` | `apiVersion`: `2025-03-01` |

## `azure-devops`

| Name | Type | Item type (`apiVersion` `itemFamily`) | Root `extra` |
| --- | --- | --- | --- |
| `gitrepositories` | `gitrepository` | `azuredevops.mia-platform.eu/v1` `gitrepositories` | `eventNames`: `git.repo.created`, `git.repo.renamed`, `git.repo.deleted` |
| `teams` | `team` | `azuredevops.mia-platform.eu/v1` `teams` | — |

## `bitbucket`

| Name | Type | Item type (`apiVersion` `itemFamily`) | Root `extra` |
| --- | --- | --- | --- |
| `pipelines` | `pipeline` | `bitbucket.mia-platform.eu/v1` `pipelines` | — |
| `repositories` | `repository` | `bitbucket.mia-platform.eu/v1` `repositories` | — |

## `console`

| Name | Type | Item type (`apiVersion` `itemFamily`) | Root `extra` |
| --- | --- | --- | --- |
| `cluster-project-relationships` | `clusterProjectRelationship` | `mia-platform.eu/v1` `relationships` | — |
| `clusters` | `cluster` | `console.mia-platform.eu/v1` `clusters` | — |
| `custom-resources` | `custom-resource` | `console.mia-platform.eu/v1` `customresources` | — |
| `projects` | `project` | `console.mia-platform.eu/v1` `projects` | — |
| `revisions` | `revision` | `console.mia-platform.eu/v1` `revisions` | — |
| `services` | `service` | `console.mia-platform.eu/v1` `services` | — |

## `gcp`

| Name | Type | Item type (`apiVersion` `itemFamily`) | Root `extra` |
| --- | --- | --- | --- |
| `buckets` | `storage.googleapis.com/Bucket` | `gcp.mia-platform.eu/v1` `buckets` | — |
| `clusters` | `container.googleapis.com/Cluster` | `gcp.mia-platform.eu/v1` `clusters` | — |
| `computeinstances` | `compute.googleapis.com/Instance` | `gcp.mia-platform.eu/v1` `virtualmachines` | — |
| `firewallrules` | `compute.googleapis.com/Firewall` | `gcp.mia-platform.eu/v1` `firewallrules` | — |
| `folders` | `cloudresourcemanager.googleapis.com/Folder` | `gcp.mia-platform.eu/v1` `folders` | — |
| `jobs` | `run.googleapis.com/Job` | `gcp.mia-platform.eu/v1` `jobs` | — |
| `networks` | `compute.googleapis.com/Network` | `gcp.mia-platform.eu/v1` `networks` | — |
| `projects` | `cloudresourcemanager.googleapis.com/Project` | `gcp.mia-platform.eu/v1` `projects` | — |
| `services` | `run.googleapis.com/Service` | `gcp.mia-platform.eu/v1` `services` | — |
| `sqlinstances` | `sqladmin.googleapis.com/Instance` | `gcp.mia-platform.eu/v1` `sqlinstances` | — |

## `github`

| Name | Type | Item type (`apiVersion` `itemFamily`) | Root `extra` |
| --- | --- | --- | --- |
| `repositories` | `repository` | `github.mia-platform.eu/v1` `repositories` | `apiVersion`: `2026-03-10` |
| `workflowruns` | `workflow_run` | `github.mia-platform.eu/v1` `workflowruns` | `apiVersion`: `2026-03-10` |

## `gitlab`

| Name | Type | Item type (`apiVersion` `itemFamily`) | Root `extra` |
| --- | --- | --- | --- |
| `accesstokens` | `accesstoken` | `gitlab.mia-platform.eu/v1` `accesstokens` | — |
| `pipelines` | `pipeline` | `gitlab.mia-platform.eu/v1` `pipelines` | — |
| `projects` | `project` | `gitlab.mia-platform.eu/v1` `projects` | — |

## `nexus`

| Name | Type | Item type (`apiVersion` `itemFamily`) | Root `extra` |
| --- | --- | --- | --- |
| `dockerimages` | `dockerimage` | `nexus.mia-platform.eu/v1` `dockerimages` | — |

## `sonarqube`

| Name | Type | Item type (`apiVersion` `itemFamily`) | Root `extra` |
| --- | --- | --- | --- |
| `issues` | `issue` | `sonarqube.mia-platform.eu/v1` `issues` | — |
| `runs` | `run` | `sonarqube.mia-platform.eu/v1` `runs` | — |

## `sysdig`

| Name | Type | Item type (`apiVersion` `itemFamily`) | Root `extra` |
| --- | --- | --- | --- |
| `vulnerabilities` | `vulnerability` | `sysdig.mia-platform.eu/v1` `vulnerabilities` | — |
