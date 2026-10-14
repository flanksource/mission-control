# Limiting Permissions

A request can be limited to less than its subject may do. A limit never grants anything: it only removes what's outside it, and the subject's own rules still decide everything else.

## Who uses it

Applications that sign their users in through an external identity provider (`external-identity-providers.md`). For example, OIPA is an application that talks to Mission Control: Mission Control has OIPA set up as an ExternalIdentityProvider and trusts the JWTs OIPA signs. Every request OIPA sends on a user's behalf carries the `X-Flanksource-Scope` header, so the request only reaches the resources it's about, whatever else the user's bindings allow.

## The `X-Flanksource-Scope` header

The `X-Flanksource-Scope` header limits one request to the Scopes it names, a JSON array of Scope ids. It's a limit, not a grant: the subject's own rules still decide, and the header removes what's outside its Scopes. Listings and checks on a resource follow one rule:

**A resource is allowed when the subject's rules allow it and it's in at least one of the header's Scopes.**

- **Listings** list a row when the subject's grants list it and it's in one of the Scopes. With Scopes of staging configs and production configs, both are listed.
- **Checks on a resource**, e.g. `read` on a config, `playbook:run`, `playbook:approve`, or resolving a connection, pass when the subject's rules allow them and every resource of the check, primary or target, is in one of the Scopes. A playbook run under the header therefore needs the playbook in one of its Scopes too, just as the playbook is only listed when it is.

A header naming no Scope, or only Scopes that select nothing, e.g. ones that don't exist, allows nothing. A resource whose membership isn't stored, e.g. a view, isn't limited, and neither are checks on a whole type, e.g. `create` on configs (`roles.md`, Section 2). Connections are limited in checks, e.g. resolving one outside the Scopes is refused, but not in listings, since their rows aren't filtered by row-level security.

The access summary of a request sent with the header is computed with its limit (`collection-access.md`, Section 4), so a type the header's Scopes select only in part is `some`, and one they don't select is `none`.

The limit applies to the requester's own request. An access review (`/rbac/subject-access-reviews`) answers by each reviewed subject's own access, without it.

Limits add up: a request made with a token limited by Roles ("Access tokens" below) and sent with the header is allowed only what both allow, so neither can widen the other.

A resource is in a Scope by its stored membership (`scopes.md`, Section 4), from the operation's snapshot (`scopes.md`, Section 7.2), so a limit adds no query.

The header can be turned off with the `auth.impersonation` property. It's then ignored.

## Access tokens

A token is limited by Roles. It may do what its owner may do, and only what one of its Roles' rules also matches:

```yaml
# POST /auth/create_token
name: ci-readonly
expiry: 720h
roles:
  - namespace: mission-control-builtin
    name: viewer
```

Alice may read, change and delete every config, run playbooks and read connections. With this token she can read configs, playbooks and components, but can't run a playbook, delete a config or read a connection: her own rules allow each of them, but no rule of `viewer` does.

Token Role references through the API MUST supply separate, nonempty `namespace` and `name` fields. The `auth token --role` CLI option MUST use `namespace/name`, e.g. `mission-control-builtin/viewer`; a bare name MUST be rejected. Both forms identify the same Role, with no fallback.

_Why:_ a token has no namespace of its own, so every Role reference must state one explicitly.

- **A limit, not a grant.** Each check of an operation (`roles.md`, Section 4.3) passes when the owner's rules allow it and a rule of one of the token's Roles matches it (`roles.md`, Section 4). A listing returns the rows both allow. A rule the owner doesn't hold adds nothing, so a token never does more than its owner.
- **Any Role, matched check by check.** A token's Roles needn't be the owner's, or bound to anyone. Each check is one action on one resource, with its target, and the owner's rules and the token's Roles must each have a rule matching that same check. Which Roles they come from doesn't matter, so neither side can pair a resource from one rule with a target from another (`roles.md`, Section 4.2).
- **Roles mean what they mean anywhere.** A Role's rules are matched against a token's checks exactly as against a binding's. A token names Roles by namespace and name, in any namespace, since a token has none. Naming one takes no grant on it: a Role in a token only removes access.
- **Every check of an operation.** An operation's checks are each limited (`roles.md`, Section 4.3), so a Role meant to let a token run a playbook on a config needs `read` on the config too.
- **Live.** Every check uses the owner's current grants and the Roles' current rules. A Role that's missing or invalid (`roles.md`, Section 6) matches nothing, so a token limited only by it can do nothing until it's valid again. Nothing is granted to the token, so nothing is left behind: when the owner loses an action, every token of theirs loses it on the next request, whatever its Roles; when the owner gains one, a token gains it only where its Roles allow it.
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

A token of Alice's limited by `payments-runner` runs the ops playbooks on `payments` configs only, and lists those configs only. The same Role limits Bob's token, who may only read `payments`, to reading them. If Alice's binding is removed, her token can run nothing, though it still lists `payments-runner`.

_Why Roles:_ a token's limit has to say which actions as well as which resources, e.g. reading playbooks but never running them, and a Role already says exactly that. A limit of Scopes alone couldn't keep a token from running a playbook it may read, and rules written into the token would be a second way to write what a Role says.

## TODO

- **Limit by Roles, not Scopes.** A Scope only says which resources, so the header can't keep a request from, e.g., running a playbook it may read. Access tokens are limited by Roles for that reason ("Access tokens" above). The header should name Roles too, by namespace and name, so that a request is limited by Roles from either source, its access token or the header, with one set of rules.
