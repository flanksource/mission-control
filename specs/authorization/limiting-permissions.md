# Limiting Permissions

A request made with an access token can be limited to less than its owner may do: every token is, except an agent's own token on requests made as that agent. A limit never grants anything: it only removes what's outside it, and the owner's own rules still decide everything else.

## Access tokens

A token is limited by the RoleBindings that select it (`token-bindings.md`). It may do what its owner may do, and only what a rule of one of its bindings' Roles also matches. A token is created with Roles, and each becomes a RoleBinding that selects it:

```yaml
# POST /auth/create_token
name: ci-readonly
expiry: 720h
roles:
  - namespace: mission-control-builtin
    name: viewer
```

They can't be changed afterwards: to change what a token may do, its owner creates a new one (`token-bindings.md`, Section 2).

Alice may read, change and delete every config, run playbooks and read connections. With this token she can read configs, playbooks and components, but can't run a playbook, delete a config or read a connection: her own rules allow each of them, but no rule of `viewer` does.

Through the API, a token names each Role with separate `namespace` and `name` fields, and both are required. `auth token --role` takes `namespace/name`, e.g. `mission-control-builtin/viewer`, and rejects a bare name.

_Why:_ a token has no namespace of its own, so a bare name has nowhere to resolve.

- **A limit, not a grant.** Each check of an operation (`roles.md`, Section 4.3) passes when the owner's rules allow it and a rule of one of the token's bindings matches it (`roles.md`, Section 4). A listing returns the rows both allow. A rule the owner doesn't hold adds nothing, so a token never does more than its owner.
- **Any Role, matched check by check.** The Roles a token is bound to needn't be the owner's, or bound to anyone else. Each check is one action on one resource, with its target, and the owner's rules and the token's bindings must each have a rule matching that same check. Which bindings they come from doesn't matter, so neither side can pair a resource from one rule with a target from another (`roles.md`, Section 4.2).
- **Roles mean what they mean anywhere.** A Role's rules are matched against a token's checks exactly as against any other binding's. A token is bound to Roles by namespace and name, in any namespace, since a token has none. Binding one takes no grant on it: a binding that selects a token never gives the token more than its owner has.
- **Every check of an operation.** An operation's checks are each limited (`roles.md`, Section 4.3), so a Role meant to let a token run a playbook on a config needs `read` on the config too.
- **Live.** Every check uses the owner's current grants and the current rules of the token's bindings. A binding whose Role is missing or invalid (`roles.md`, Section 6) matches nothing, so a token limited only by it can do nothing until it's valid again. Nothing is granted to the token, so nothing is left behind: when the owner loses an action, every token of theirs loses it on the next request, whatever its bindings; when the owner gains one, a token gains it only where its bindings allow it.
- **The summary follows the token.** The access summary of a request made with a token is computed with the token's limit (`collection-access.md`, Section 4).

For example, `production` selects the configs of the `payments`, `billing` and `search` namespaces, and `production-payments` those of `payments` alone. Alice is bound to a Role that runs the ops playbooks on `production` and reads it. An admin publishes a Role bound to no one:

```yaml
kind: Role
metadata:
  name: payments-runner
  namespace: default
spec:
  rules:
    - name: run-ops
      action: playbook:run
      resource:
        scopeRef: ops-playbooks
      target:
        scopeRef: production-payments
    - name: read-payments
      action: read
      resource:
        scopeRef: production-payments
```

A token of Alice's bound to `payments-runner` runs the ops playbooks on `payments` configs only, and lists those configs only. The same Role limits Bob's token, who may only read `payments`, to reading them. If Alice's own binding is removed, her token can run nothing, though it's still bound to `payments-runner`.

_Why Roles:_ a token's limit has to say which actions as well as which resources, e.g. reading playbooks but never running them, and a Role already says exactly that. A limit of Scopes alone couldn't keep a token from running a playbook it may read, and rules written into the token would be a second way to write what a Role says. Binding the Role with a RoleBinding, rather than listing Roles on the token, keeps one way to bind a Role (`token-bindings.md`).

_Why no per-request limit:_ an application that signs its users in through an external identity provider (`external-identity-providers.md`) already decides what each of its requests may reach: the claims of each token it signs select the RoleBindings that apply, and a binding's constraint narrows its Role to a Scope (`rolebindings.md`, Section 3). That limits actions as well as resources, which a list of Scopes couldn't. Narrowing this way needs a binding for each context the application narrows to, matched on a claim it sets: unlike the header, it can't narrow to an arbitrary Scope on the fly.
