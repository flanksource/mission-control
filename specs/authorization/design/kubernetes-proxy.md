# Kubernetes Proxy

**Status: postponed. Not adopted.** The specs leave the Kubernetes proxy without an action (`overview.md`, "Not covered yet"), so it doesn't work under the new model. This records the design reached so far, so the work can resume from it.

## Background

Mission Control can hand a person a kubeconfig that sends `kubectl` requests through its `/kubeproxy` endpoint. Today the proxy only reaches the cluster Mission Control runs in. It authenticates with Mission Control's own service account and impersonates one of two fixed Kubernetes users, a reader and a writer, chosen by the person's built-in role. Built-in roles go away, so the proxy needs an action like everything else.

The proxy can be granted on a new resource type for the local cluster, on the cluster's config, on an agent, through Kubernetes RBAC by impersonating the person, or on a Kubernetes connection. We chose the connection, because it needs no new type and no exception to "every rule has a resource", and it turns the proxy into a general one for every cluster Mission Control has a connection to.

## Design

- **Action.** `kubernetes:proxy`, on Connection, with no target. It lets a subject send Kubernetes API requests through Mission Control's proxy, with that connection's credentials.
- **Scope.** The rule's `resource` Scope MUST select Kubernetes connections only: every target is a `connection` target with `types: [kubernetes]`. A Role whose Scope selects another type, or other connections, is `Ready=False`. The action is checked per request and never lists connections, so a Scope may select some Kubernetes connections and not others.
- **Subject.** The person. The kubeconfig carries the person's own access token, so every proxied request is checked as them, against the one connection it goes through.
- **Kubeconfig.** The download offers the Kubernetes connections the subject may proxy to, one context each. Each context points at a proxy endpoint that names its connection, e.g. `/kubeproxy/<namespace>/<name>`.
- **Separate from `connection:use`.** `connection:use` lets a playbook or plugin act with the credentials in Mission Control's own code. `kubernetes:proxy` gives a person the whole Kubernetes API with them. Neither implies the other.
- **Reader and writer.** What a proxied request may do is decided by the connection's credentials and the cluster's own RBAC. Two levels of access to one cluster are two connections, e.g. `local-reader` and `local-writer`.
- **The local cluster.** Bootstrap creates the connections for the cluster Mission Control runs in.

Example:

```yaml
kind: Scope
metadata:
  name: prod-clusters
  namespace: mission-control
spec:
  targets:
    - connection:
        types: [kubernetes]
        namespace: prod
---
kind: Role
metadata:
  name: prod-kube-access
  namespace: mission-control
spec:
  rules:
    - name: proxy
      action: kubernetes:proxy
      resource:
        scopeRef: prod-clusters
---
kind: RoleBinding
metadata:
  name: platform-prod-kube
  namespace: mission-control
spec:
  role: prod-kube-access
  subjects:
    teams: [platform]
```

## Open questions

- **Credentials for the local cluster.** A Kubernetes connection only holds a kubeconfig today. Bootstrap would either store a kubeconfig with a long-lived service account token in a Secret, a downgrade from today's rotated token that never leaves the pod, or Kubernetes connections would get an in-cluster mode, using Mission Control's own service account, and a field naming the Kubernetes user to impersonate.
- **Reaching other clusters.** Whether Mission Control can reach every cluster it has a connection to, e.g. clusters behind an agent.

## Other choices

- **A new type for the local cluster.** Rejected: a type with one member, for one feature.
- **An action on the cluster's config.** Rejected: it works only where Mission Control scrapes its own cluster.
- **An action on the `local` agent.** Rejected: an agent isn't what's being accessed.
- **Impersonating the person, with Kubernetes RBAC deciding.** Rejected: access to the cluster would be managed outside Mission Control.
- **A rule with no resource, as Kubernetes does for `nonResourceURLs`.** Rejected: an exception to "every rule has a resource".
