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

Limits add up: a request made with a token limited by Roles (`roles.md`, Section 4.5) and sent with the header is allowed only what both allow, so neither can widen the other.

A resource is in a Scope by its stored membership (`scopes.md`, Section 4), from the operation's snapshot (`scopes.md`, Section 7.2), so a limit adds no query.

The header can be turned off with the `auth.impersonation` property. It's then ignored.

## TODO

- **Limit by Roles, not Scopes.** A Scope only says which resources, so the header can't keep a request from, e.g., running a playbook it may read. Access tokens are limited by Roles for that reason (`roles.md`, Section 4.5). The header should name Roles too, by namespace and name, so that a request is limited by Roles from either source, its access token or the header, with one set of rules.
- **Move the access token limit here.** Once both are limited by Roles, the rules in `roles.md`, Section 4.5 for how a token's Roles limit its checks belong in this document, and `roles.md` keeps only what an access token is.
