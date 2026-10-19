# Kubernetes Source

The Kubernetes source connects IBDM to the API server of a [Kubernetes](https://kubernetes.io/) cluster to
synchronise the cluster, its namespaces, workloads, services, network policies, Helm releases, Traefik ingress
routes and cert-manager certificates into the Mia-Platform Catalog. It supports both a one-off sync and a
long-running watch that streams every change as it happens.

The source only ever **reads** from the cluster: it uses `list`, `get` and `watch` calls and never creates, updates or
deletes any Kubernetes object.

One IBDM instance monitors one cluster. To monitor several clusters, run one instance per cluster, each with its own
connection settings.

## Commands

### Sync

```bash
ibdm sync k8s --mapping-file <path to mapping file or folder>
```

Performs a one-off synchronisation: lists every resource of the types you configured and exits. Use it for an initial
load, for scheduled runs, or for trying a mapping with `--local-output`.

### Run (Watch)

```bash
ibdm run k8s --mapping-file <path to mapping file or folder>
```

Starts a long-running process that watches the cluster. It first sends everything that already exists, like a sync, and
then sends every creation, change and deletion as it happens. See [Sync and Watch Behaviour](#sync-and-watch-behaviour).

## Configuration

All configuration is read from environment variables. The source supports two **mutually exclusive** ways of
connecting; exactly one must be configured.

### Token Mode (Recommended)

IBDM connects to the API server with a ServiceAccount bearer token. Set `K8S_API_SERVER` and `K8S_BEARER_TOKEN`, and
preferably `K8S_CA_CERT` so that the API server certificate is verified.

### Kubeconfig Mode

IBDM loads the connection from a kubeconfig file. Set `K8S_KUBECONFIG_PATH`, and optionally
`K8S_KUBECONFIG_CONTEXT` to pick a context other than the current one. Authentication plugins configured in the
kubeconfig (`exec` plugins, for example the ones used for OIDC or cloud providers) are run by IBDM, so their binary must
be available where IBDM runs and they must work without asking for input: IBDM never prompts, so a login that needs a
browser has to be completed (and its token cached) beforehand. The legacy `auth-provider` entries (such as `oidc`) are no longer supported by the Kubernetes
client library and must be replaced by an `exec` plugin.

Kubeconfig mode uses the credentials of whoever created the kubeconfig, so it is better suited to trying the source
than to production.

Setting variables of both modes is a configuration error.

### Environment Variables

| Env Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `K8S_API_SERVER` | Token mode | _(empty)_ | URL of the API server, for example `https://34.0.0.1`. Must be an `http` or `https` URL. |
| `K8S_BEARER_TOKEN` | Token mode | _(empty)_ | ServiceAccount token used to authenticate. |
| `K8S_CA_CERT` | No | _(empty)_ | Base64 encoded PEM bundle of the certificate authority of the API server. |
| `K8S_INSECURE_SKIP_VERIFY` | No | `false` | Skips the verification of the API server certificate. For local tests only. |
| `K8S_CLUSTER_NAME` | No | host of the API server | Name given to the `cluster` item. |
| `K8S_KUBECONFIG_PATH` | Kubeconfig mode | _(empty)_ | Path of the kubeconfig file. |
| `K8S_KUBECONFIG_CONTEXT` | No | current context | Context of the kubeconfig to use. Requires `K8S_KUBECONFIG_PATH`. |

`K8S_CA_CERT` and `K8S_INSECURE_SKIP_VERIFY` are mutually exclusive. Every request to the API server has a timeout of
30 seconds, and lists are read in pages of 500 objects.

## Preparing the Cluster

IBDM needs a ServiceAccount with read-only access to the resources you want to synchronise, and a token for it.

### Read-Only Access

Apply the following manifest on the cluster to create the ServiceAccount and a read-only ClusterRole:

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: ibdm-k8s-reader
  namespace: default
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: ibdm-k8s-reader
rules:
  - apiGroups: [""]
    resources: ["namespaces", "nodes", "services", "pods"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["apps"]
    resources: ["deployments", "statefulsets", "daemonsets"]
    verbs: ["get", "list", "watch"]
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["networking.k8s.io"]
    resources: ["networkpolicies"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["traefik.io"]
    resources: ["ingressroutes"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["cert-manager.io"]
    resources: ["certificates"]
    verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: ibdm-k8s-reader
subjects:
  - kind: ServiceAccount
    name: ibdm-k8s-reader
    namespace: default
roleRef:
  kind: ClusterRole
  name: ibdm-k8s-reader
  apiGroup: rbac.authorization.k8s.io
```

Grant only what you need: each resource is only used by the data types that read it, so you can drop the rules of the
types you do not synchronise. The `watch` verb is only needed by `ibdm run`.

> [!WARNING]
> The `secrets` rule is needed only by the `helmrelease` type, which reads the Secrets where Helm stores its releases.
> Kubernetes cannot restrict a role to Secrets with a given label, so this rule gives the ServiceAccount read access to
> **every Secret in the cluster**, even though IBDM only requests the ones labelled `owner=helm`. If this is not
> acceptable for the cluster, remove the rule and do not use the `helmrelease` type.

A resource whose definition is not installed on the cluster (Traefik ingress routes or cert-manager certificates) is
skipped without error, so granting the rule for it is harmless.

### Creating a Long-Lived Token

For anything that runs continuously, use a token that does not expire. Create a Secret of type
`kubernetes.io/service-account-token` for the ServiceAccount:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: ibdm-k8s-reader-token
  namespace: default
  annotations:
    kubernetes.io/service-account.name: ibdm-k8s-reader
type: kubernetes.io/service-account-token
```

Kubernetes fills the Secret with the token and the certificate authority of the cluster. Read them, and the API server
address, with:

```bash
# K8S_BEARER_TOKEN
kubectl -n default get secret ibdm-k8s-reader-token -o jsonpath='{.data.token}' | base64 -d

# K8S_CA_CERT (already base64 encoded)
kubectl -n default get secret ibdm-k8s-reader-token -o jsonpath='{.data.ca\.crt}'

# K8S_API_SERVER
kubectl config view --minify -o jsonpath='{.clusters[0].cluster.server}'
```

Store the token in a secret manager or in a Kubernetes Secret of the cluster where IBDM runs, and inject it as an
environment variable. Never keep it in a file of a repository.

The token is read once, when IBDM starts. This is enough for a token that does not expire.

**Rotating the token**: create a second Secret, update `K8S_BEARER_TOKEN` and restart IBDM, then delete the old Secret.

**Revoking the token**: delete the Secret. The token stops working immediately.

> [!NOTE]
> Short-lived tokens (`kubectl create token ibdm-k8s-reader --duration=1h`) are fine for a one-off `ibdm sync` or for
> trying the source, but `ibdm run` does not reload its token: it starts failing with authentication errors when the
> token expires.

## Supported Data Types

The types to synchronise are chosen by the mapping files you pass to IBDM: a type is only synchronised when a mapping
file for it is present. The ready-made mappings live in `docs/mappings/k8s`, and you can pass the whole folder or copy
only the files you need.

| Mapping file | Type | Item kind | Sync | Run |
| --- | --- | --- | --- | --- |
| `clusters.yaml` | `cluster` | `K8sCluster` | ✅ | ✅ |
| `namespaces.yaml` | `namespace` | `Namespace` | ✅ | ✅ |
| `deployments.yaml` | `deployment` | `Deployment` | ✅ | ✅ |
| `statefulsets.yaml` | `statefulset` | `StatefulSet` | ✅ | ✅ |
| `daemonsets.yaml` | `daemonset` | `DaemonSet` | ✅ | ✅ |
| `services.yaml` | `service` | `Service` | ✅ | ✅ |
| `networkpolicies.yaml` | `networkpolicy` | `NetworkPolicy` | ✅ | ✅ |
| `helmreleases.yaml` | `helmrelease` | `HelmRelease` | ✅ | ✅ |
| `ingressroutes.yaml` | `ingressroute` | `IngressRoute` | ✅ | ✅ |
| `certificates.yaml` | `certificate` | `Certificate` | ✅ | ✅ |
| `pods.yaml` | `pod` | `Pod` | ✅ | ✅ |

Every item type has to be registered in the Catalog before IBDM sends items of that type.

### What Each Type Carries

| Type | Main fields |
| --- | --- |
| `cluster` | `name`, `nodeCount`, `totalCPUCores`, and `nodes`: the list of nodes with `name`, `kubeletVersion`, `osImage`, `kernelVersion`, `architecture`, `cpuCapacity` and `taints`. |
| `namespace` | `name`, `labels`. |
| `deployment`, `statefulset`, `daemonset` | `name`, `namespace`, `labels`, `replicas` (not for daemonsets), `containers` with `name`, `image` and `resources` (`requests` and `limits` as raw Kubernetes quantities), `initContainers` with `name` and `image`. |
| `service` | `name`, `namespace`, `labels`, `type`, `clusterIPs`, `ports`, `loadBalancer` addresses, `externalIPs`, `externalName`, `selector`. |
| `networkpolicy` | `name`, `namespace`, `labels`, `podSelector`, the effective `policyTypes`, and the full `ingress` and `egress` rules with their peers and ports. |
| `helmrelease` | `name`, `namespace`, `revision`, `status`, `chartName`, `chartVersion`, `appVersion`, `firstDeployed`, `lastDeployed`. The release values are never read. |
| `ingressroute` | `name`, `namespace`, `labels`, `entryPoints`, `routes` with their match rule, services and middlewares, and the `tls` settings. |
| `certificate` | `name`, `namespace`, `labels`, `dnsNames`, `issuer`, `status` and `statusReason` of the `Ready` condition, `renewalTime`, `notAfter`. |
| `pod` | `name`, `namespace`, `labels`, `annotations`, `owner`, `nodeName`, `phase`, `podIP`, `startTime`, `containers` with the running `imageID`, `resources` and `securityContext`, the service account, `volumes` (type and referenced name only), `tolerations`, `nodeSelector`, the host network flags and the pod security context. |

The mapping files are the reference for the exact fields. Environment variables, Secret and volume contents are never
read into any item.

Pods that completed (phase `Succeeded`) are not synchronised, and fields that change
all the time, such as the readiness, the restart count and the container state, are not part of the item.

### Relationships

Every namespaced item is linked to its namespace with a `part-of` relationship, and every namespace is linked to the
cluster. The relationships that cannot be expressed inside the mapping of a single item have their own types, and
like every type they are synchronised only when their mapping file is present:

| Mapping file | Type | Relationship |
| --- | --- | --- |
| `ingressroute-service-relationships.yaml` | `ingressRouteServiceRelationship` | `IngressRoute` `dependency` `Service`. Only `Service` targets are linked, and a route that points to a Traefik internal service (such as `api@internal`) gets no link. |
| `workload-helmrelease-relationships.yaml` | `workloadHelmReleaseRelationship` | Workload `part-of` `HelmRelease`, from the `meta.helm.sh/release-name` and `meta.helm.sh/release-namespace` annotations that Helm adds to the objects it creates. |
| `service-workload-relationships.yaml` | `serviceWorkloadRelationship` | `Service` `dependency` workload, when the selector of the service matches the pod template labels of the workload in the same namespace. |
| `pod-owner-relationships.yaml` | `podOwnerRelationship` | `Pod` `part-of` its Deployment, StatefulSet or DaemonSet. Pods owned by anything else, such as a Job, are not linked. |

These links connect items by identifier, so they only resolve to something when the item types on both ends are
synchronised too.

## Cluster Identity and the Console Link

The items of a cluster get their identifiers from the address configured in `K8S_API_SERVER`, without the `https://`
prefix. In particular, the `cluster` item also creates a `dependency` relationship from the Mia-Platform Console
`Cluster` item to the Kubernetes one, computing the identifier of the Console item from the same address.

For that relationship to point to a real item, `K8S_API_SERVER` must be **exactly** the connection URL registered for the
cluster in the Console, without `https://`. A different spelling, such as a host name instead of an IP address, or an
address with a port (`…:443`) when the Console has none, produces a different identifier: the synchronisation still
works, but the link points to nothing.

The same address is part of every identifier, so changing it creates new items instead of updating the existing ones.

## Sync and Watch Behaviour

### Sync

A sync lists every requested type in pages. A failure while reading one type is logged and does not stop the other
types, and the failures are reported when the sync ends. A type with no mapping file is skipped, and a custom resource
that is not installed on the cluster is skipped with an informational log.

A sync only creates and updates items: it never removes the ones of objects that no longer exist. Use `ibdm run` to
keep the Catalog aligned with deletions.

### Run

`ibdm run k8s` keeps one watch open for every requested type, and the first thing it sends is the full set of existing
objects. After that:

- Creating or changing an object sends an update, deleting it sends a deletion of the item and of its relationships.
- A change that does not alter any field of the item (for example a status change of a deployment) sends nothing.
- The `cluster` item is sent once at the start and again whenever the data of the nodes changes. It is never deleted.
- A Helm upgrade or rollback updates the release item, and does not delete it. A release is deleted only when it has
  no deployed revision left and Helm is not writing a new one, as happens when the release is uninstalled.
- A Pod that completes (phase `Succeeded`) is removed from the Catalog together with its relationships.
- Relationships that depend on both ends, such as the one between a service and its workloads, are recomputed when either
  side changes, adding the new links and removing the ones that no longer match.
- Connections that drop are re-established automatically. A watch that cannot start or sync is logged, and the other
  types keep working.

There is no periodic full resync: the watches are the only source of changes.

## Limitations

- Helm releases are read from Secrets (Helm 3 default). Releases stored in ConfigMaps and Helm 2 releases are not found.
- Only the `traefik.io` API group is supported for ingress routes. The legacy `traefik.containo.us` group is not.
- Services are linked to workloads through the pod template labels, not through the running pods, and a service without a
  selector gets no link. Selectors that depend on labels set at runtime on the pods are not matched.
- Nodes are part of the `cluster` item and do not have an item of their own.
- Init containers carry only their name and image, without resources.
- The cluster name is the host of the API server unless `K8S_CLUSTER_NAME` is set.

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| `the server has asked for the client to provide credentials` | The token is wrong, expired, or belongs to another cluster. Check it with `curl -sk -H "Authorization: Bearer $K8S_BEARER_TOKEN" "$K8S_API_SERVER/api/v1/namespaces"`. |
| `forbidden` errors for some types | The ServiceAccount lacks the rule for that resource. Add it to the ClusterRole. |
| `informer did not sync in time` | The cluster cannot be reached or is slow. It keeps retrying, so check the network between IBDM and the API server. |
| Nothing is printed for a type | The mapping file for that type is missing, or its custom resource is not installed. |
| The Console link points to nothing | `K8S_API_SERVER` differs from the URL registered in the Console. See [Cluster Identity](#cluster-identity-and-the-console-link). |
| TLS errors | Set `K8S_CA_CERT`, or use `K8S_INSECURE_SKIP_VERIFY=true` for tests only. |
| The cluster uses OIDC and token mode fails | The address may be an authentication proxy that rejects ServiceAccount tokens. Use the address of the API server itself, or kubeconfig mode with an `exec` plugin. |

To see what IBDM would send without needing Catalog credentials, add `--local-output`, and use `--log-level=TRACE` for
detailed logs.
