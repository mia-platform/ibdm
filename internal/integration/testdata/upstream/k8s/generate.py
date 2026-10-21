#!/usr/bin/env python3
"""Generates the Kubernetes API fixtures of the k8s integration tests.

Run from this directory: python3 generate.py. Every value is fictional. The Helm release is
encoded as Helm stores it: gzip (fixed mtime, so the bytes are stable) then base64, and the
Kubernetes API encodes Secret data in base64 once more.
"""
import base64, gzip, io, json

def dump(name, obj):
    with open(name, "w") as f:
        json.dump(obj, f, indent=2)
        f.write("\n")

def lst(kind, api_version, items, cont=""):
    meta = {"resourceVersion": "1000"}
    if cont:
        meta["continue"] = cont
    return {"kind": kind, "apiVersion": api_version, "metadata": meta, "items": items}

created = "2026-03-01T09:00:00Z"
def meta(name, namespace=None, labels=None, annotations=None, owners=None, uid=None):
    m = {"name": name, "uid": uid or ("uid-" + name), "creationTimestamp": created, "resourceVersion": "1"}
    if namespace:
        m["namespace"] = namespace
    if labels:
        m["labels"] = labels
    if annotations:
        m["annotations"] = annotations
    if owners:
        m["ownerReferences"] = owners
    return m

container = lambda name, image: {"name": name, "image": image,
    "resources": {"requests": {"cpu": "250m", "memory": "256Mi"}, "limits": {"cpu": "1", "memory": "512Mi"}}}

# Cluster nodes
dump("nodes.json", lst("NodeList", "v1", [
    {"metadata": meta("worker-a", labels={"kubernetes.io/os": "linux"}),
     "spec": {"taints": [{"key": "dedicated", "value": "databases", "effect": "NoSchedule"}]},
     "status": {"capacity": {"cpu": "4", "memory": "16Gi"},
                "nodeInfo": {"kubeletVersion": "v1.31.2", "osImage": "Ubuntu 24.04 LTS", "kernelVersion": "6.8.0-45-generic", "architecture": "amd64"}}},
    {"metadata": meta("worker-b"),
     "spec": {},
     "status": {"capacity": {"cpu": "2500m", "memory": "8Gi"},
                "nodeInfo": {"kubeletVersion": "v1.31.2", "osImage": "Ubuntu 24.04 LTS", "kernelVersion": "6.8.0-45-generic", "architecture": "arm64"}}},
]))

# Namespaces, over two pages
dump("namespaces-page-1.json", lst("NamespaceList", "v1", [
    {"metadata": meta("storefront", labels={"team": "storefront"}), "spec": {}, "status": {"phase": "Active"}}], cont="namespaces-page-2"))
dump("namespaces-page-2.json", lst("NamespaceList", "v1", [
    {"metadata": meta("platform", labels={"team": "platform"}), "spec": {}, "status": {"phase": "Active"}}]))

helm = {"meta.helm.sh/release-name": "catalog-api", "meta.helm.sh/release-namespace": "storefront"}
dump("deployments.json", lst("DeploymentList", "apps/v1", [
    {"metadata": meta("catalog-api", "storefront", labels={"app": "catalog-api", "app.kubernetes.io/managed-by": "Helm"}, annotations=helm),
     "spec": {"replicas": 2, "selector": {"matchLabels": {"app": "catalog-api"}},
              "template": {"metadata": {"labels": {"app": "catalog-api"}},
                           "spec": {"containers": [container("api", "registry.example.com/acme-widgets/catalog-api:1.4.0")],
                                    "initContainers": [{"name": "migrate", "image": "registry.example.com/acme-widgets/catalog-api-migrate:1.4.0"}]}}}}]))
dump("statefulsets.json", lst("StatefulSetList", "apps/v1", [
    {"metadata": meta("orders-db", "storefront", labels={"app": "orders-db"}),
     "spec": {"replicas": 1, "serviceName": "orders-db", "selector": {"matchLabels": {"app": "orders-db"}},
              "template": {"metadata": {"labels": {"app": "orders-db"}},
                           "spec": {"containers": [container("postgres", "registry.example.com/library/postgres:16.4")]}}}}]))
dump("daemonsets.json", lst("DaemonSetList", "apps/v1", [
    {"metadata": meta("log-agent", "platform", labels={"app": "log-agent"}),
     "spec": {"selector": {"matchLabels": {"app": "log-agent"}},
              "template": {"metadata": {"labels": {"app": "log-agent"}},
                           "spec": {"containers": [container("agent", "registry.example.com/platform/log-agent:3.2.1")]}}}}]))

dump("services.json", lst("ServiceList", "v1", [
    {"metadata": meta("catalog-api", "storefront", labels={"app": "catalog-api"}),
     "spec": {"type": "ClusterIP", "clusterIPs": ["10.96.10.20"], "selector": {"app": "catalog-api"},
              "ports": [{"name": "http", "protocol": "TCP", "port": 80, "targetPort": 8080}]}, "status": {}},
    {"metadata": meta("orders-db", "storefront"),
     "spec": {"type": "ClusterIP", "clusterIPs": ["None"], "selector": {"app": "orders-db"},
              "ports": [{"name": "postgres", "protocol": "TCP", "port": 5432, "targetPort": 5432}]}, "status": {}},
    {"metadata": meta("legacy-billing", "storefront"),
     "spec": {"type": "ExternalName", "externalName": "billing.example.com"}, "status": {}},
]))

dump("networkpolicies.json", lst("NetworkPolicyList", "networking.k8s.io/v1", [
    {"metadata": meta("catalog-api-ingress", "storefront"),
     "spec": {"podSelector": {"matchLabels": {"app": "catalog-api"}}, "policyTypes": ["Ingress"],
              "ingress": [{"from": [{"namespaceSelector": {"matchLabels": {"team": "platform"}}}],
                           "ports": [{"protocol": "TCP", "port": 8080}]}]}}]))

release = {"name": "catalog-api", "namespace": "storefront", "version": 3,
           "info": {"status": "deployed", "first_deployed": "2026-01-10T09:00:00Z", "last_deployed": "2026-09-30T14:00:00Z"},
           "chart": {"metadata": {"name": "catalog-api", "version": "1.4.0", "appVersion": "1.4.0"}},
           "config": {"replicas": 2}}
buf = io.BytesIO()
with gzip.GzipFile(fileobj=buf, mode="wb", mtime=0) as gz:
    gz.write(json.dumps(release, separators=(",", ":")).encode())
helm_encoded = base64.b64encode(buf.getvalue())
dump("helm-secrets.json", lst("SecretList", "v1", [
    {"metadata": meta("sh.helm.release.v1.catalog-api.v3", "storefront",
                      labels={"owner": "helm", "status": "deployed", "name": "catalog-api", "version": "3"}),
     "type": "helm.sh/release.v1", "data": {"release": base64.b64encode(helm_encoded).decode()}}]))

def pod(name, namespace, owners, labels, phase, container_list):
    return {"metadata": meta(name, namespace, labels=labels, owners=owners),
            "spec": {"nodeName": "worker-a", "serviceAccountName": "default", "containers": container_list},
            "status": {"phase": phase, "podIP": "10.244.1.7", "startTime": "2026-09-30T14:01:00Z",
                       "containerStatuses": [{"name": c["name"], "imageID": c["image"].split(":")[0] + "@sha256:" + "b" * 64, "ready": True, "restartCount": 0, "image": c["image"]} for c in container_list]}}
own = lambda kind, name: [{"apiVersion": "apps/v1" if kind != "Job" else "batch/v1", "kind": kind, "name": name, "uid": "uid-" + name, "controller": True}]
dump("pods.json", lst("PodList", "v1", [
    pod("catalog-api-7d9f8b6c4-x2k9p", "storefront", own("ReplicaSet", "catalog-api-7d9f8b6c4"),
        {"app": "catalog-api", "pod-template-hash": "7d9f8b6c4"}, "Running", [container("api", "registry.example.com/acme-widgets/catalog-api:1.4.0")]),
    pod("orders-db-0", "storefront", own("StatefulSet", "orders-db"), {"app": "orders-db"}, "Running",
        [container("postgres", "registry.example.com/library/postgres:16.4")]),
    pod("nightly-report-29012345-q8w7e", "platform", own("Job", "nightly-report-29012345"), {"job-name": "nightly-report-29012345"}, "Running",
        [container("report", "registry.example.com/platform/report:0.3.0")]),
    pod("db-migration-29012300-z1x2c", "storefront", own("Job", "db-migration-29012300"), {"job-name": "db-migration-29012300"}, "Succeeded",
        [container("migrate", "registry.example.com/acme-widgets/catalog-api-migrate:1.4.0")]),
]))

# Discovery
dump("api-versions.json", {"kind": "APIVersions", "versions": ["v1"], "serverAddressByClientCIDRs": [{"clientCIDR": "0.0.0.0/0", "serverAddress": "k8s-api.example.com:443"}]})
group = lambda name, version: {"name": name, "versions": [{"groupVersion": name + "/" + version, "version": version}], "preferredVersion": {"groupVersion": name + "/" + version, "version": version}}
dump("api-groups.json", {"kind": "APIGroupList", "apiVersion": "v1", "groups": [group("apps", "v1"), group("networking.k8s.io", "v1"), group("traefik.io", "v1alpha1"), group("cert-manager.io", "v1")]})
dump("api-groups-without-traefik.json", {"kind": "APIGroupList", "apiVersion": "v1", "groups": [group("apps", "v1"), group("networking.k8s.io", "v1"), group("cert-manager.io", "v1")]})
res = lambda gv, name, kind: {"kind": "APIResourceList", "apiVersion": "v1", "groupVersion": gv,
    "resources": [{"name": name, "singularName": "", "namespaced": True, "kind": kind, "verbs": ["get", "list", "watch"]}]}
dump("traefik-resources.json", res("traefik.io/v1alpha1", "ingressroutes", "IngressRoute"))
dump("cert-manager-resources.json", res("cert-manager.io/v1", "certificates", "Certificate"))

dump("ingressroutes.json", {"apiVersion": "traefik.io/v1alpha1", "kind": "IngressRouteList", "metadata": {"resourceVersion": "1000"}, "items": [
    {"apiVersion": "traefik.io/v1alpha1", "kind": "IngressRoute", "metadata": meta("storefront", "storefront", labels={"app": "catalog-api"}),
     "spec": {"entryPoints": ["websecure"],
              "routes": [{"match": "Host(`shop.example.com`) && PathPrefix(`/api`)", "kind": "Rule",
                          "services": [{"name": "catalog-api", "port": 80}], "middlewares": [{"name": "rate-limit"}]},
                         {"match": "Host(`shop.example.com`) && PathPrefix(`/dashboard`)", "kind": "Rule",
                          "services": [{"name": "api@internal", "kind": "TraefikService"}]}],
              "tls": {"secretName": "shop-example-com-tls"}}}]})
dump("certificates.json", {"apiVersion": "cert-manager.io/v1", "kind": "CertificateList", "metadata": {"resourceVersion": "1000"}, "items": [
    {"apiVersion": "cert-manager.io/v1", "kind": "Certificate", "metadata": meta("shop-example-com", "storefront"),
     "spec": {"secretName": "shop-example-com-tls", "dnsNames": ["shop.example.com"], "issuerRef": {"name": "letsencrypt", "kind": "ClusterIssuer"}},
     "status": {"conditions": [{"type": "Ready", "status": "True", "reason": "Ready"}], "notAfter": "2026-12-29T09:00:00Z", "renewalTime": "2026-11-29T09:00:00Z"}}]})
dump("status-error.json", {"kind": "Status", "apiVersion": "v1", "metadata": {}, "status": "Failure", "message": "the fake API server failed", "reason": "InternalError", "code": 500})
