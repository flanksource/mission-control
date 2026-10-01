# Playbook Specification

This spec covers what a playbook runs on. Other parts of playbooks aren't specified here yet.

## 1. What a playbook runs on

A playbook declares the resources it runs on with three selector lists: `configs`, `components` and `checks`.

```yaml
apiVersion: mission-control.flanksource.com/v1
kind: Playbook
metadata:
  name: restart-pod
spec:
  configs:
    - types:
        - Kubernetes::Pod
```

- A playbook that declares at least one selector **runs on a resource**. Each of its runs is on exactly one config, component or check.
- A playbook that declares none **runs on its own**. Its runs have no resource.

A playbook is one or the other. To run the same steps both ways, write two playbooks.

## 2. A run must fit its playbook

A run that doesn't fit its playbook MUST be rejected with `400 Bad Request`, before any permission is checked:

- A playbook that runs on a resource MUST be given one. The resource MUST be of a type the playbook declares, and MUST match at least one of the playbook's selectors for that type.
- A playbook that runs on its own MUST NOT be given a resource.

For `restart-pod` above:

| Run                                  | Result                           |
| ------------------------------------ | -------------------------------- |
| On a `Kubernetes::Pod` config        | Accepted, then authorized        |
| With no resource                     | Rejected: needs a resource       |
| On a `Kubernetes::Deployment` config | Rejected: matches no selector    |
| On a component                       | Rejected: declares no components |

`filters` still apply to runs that fit. They can narrow which resources a playbook runs on further, but they don't replace this check.

## 3. Why

- A playbook is written for what it declares. `restart-pod` run without a pod, or on a component, does whatever its templates happen to do with a missing or unexpected resource.
- Authorization depends on it. A Role checks a run against its target, and checks a run without one differently (`authorization/roles.md`, Section 4.1). If the caller could choose whether a run has a target, they could choose which checks it faces.

## 4. Which runs this applies to

It applies to every run a subject requests: through the API, the UI or MCP.

Runs started by the playbook's own triggers (`on`) and by its schedule aren't requested by a subject. Whether they must fit as well is an open question.

## 5. Status

Not enforced yet. Today the selectors are only used to list the playbooks that apply to a resource, and a run may be given any resource, or none.
