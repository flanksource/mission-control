# Operation Gates

Details how operations on resource types are gated, for `collection-access.md` (Section 3.1).

## Background

An operation's gate can be checked inside its handler, or declared with its route. This design declares it with the route, in the same place as the row filtering that makes a `some` gate safe. _Why:_ a `some` gate is only correct while the operation really filters, and a gate declared apart from the filtering drifts from it unnoticed. Declared with the route, the gate of every operation can be read from one route table.

## Design

- **One answer.** A gate reads the subject's answer for the type and action from the access summary (`collection-access.md`, Section 4).
- **Cache.** Answers are cached per subject, access token Roles, `X-Flanksource-Scope` header and, for a user of an external identity provider, the set of `oidc` subjects their token matches. Every cached answer is dropped when a Permission, PermissionGroup, Scope, Role, RoleBinding, team membership, agent or a resource that's a subject (a playbook, notification, topology, scraper, canary or plugin) changes. _Why:_ the key covers what's read per request; the events cover what's stored. A token's other claims, such as its expiry, aren't in the key, so a new token that matches the same bindings reuses the answer. Dropping everything on any change is coarse, but these changes are rare, and a partial drop can miss a subject.
- **Declaring a gate.** A route or MCP tool names its type, its action, and its gate:
  - `some` together with row filtering, as one declaration. The operation's queries run under the subject's row filters.
  - `some` for an operation that checks the subject's rules against the resource in hand. The declaration says so, and is reviewed against the handler.
  - `all` for anything else.
- **Database API.** Each table maps to a resource type, and records to their resource's type (`roles.md`, Section 3.1). A read of a table is gated at `some` only once the table's row filtering is in place and exercised by the tests below; until then it's gated at `all`. Views and functions are gated at `some` only when they run as the caller, so the row filters apply to them. _Why:_ a table is safe at `some` only once its filtering is shown to work; until then, `all` refuses rather than leaks.
- **Other services.** Operations forwarded to another service, such as config-db or canary-checker, are gated at `all`. _Why:_ those services query over their own connections, so neither row filters nor the request's limits apply.
- **Shared caches.** A handler gated at `some` MUST NOT return a resource read from a cache shared between subjects, unless it checks the subject's rules against that resource first. _Why:_ a cache hit never reaches the database, so the row filters never apply to it.
- **Tests.** Every gated route and MCP tool is exercised by subjects whose answer for its type is `some`: one granted part of the type, and one granted all of it but sending an `X-Flanksource-Scope` header that selects part of it. A resource outside their grants or limit MUST never come back, including after another subject has read it. _Why:_ this is the only check that a `some` declaration is true, and it catches caches that serve another subject's reads.

## Other choices

- **Keep whole-type object checks.** A subject granted part of a type fails them, so they'd be refused the listings `collection-access.md` says must serve them, or each route would need a second path for them.
- **Gate every operation at `some`.** Operations served by other services can't filter, so they'd show resources the subject wasn't granted.
- **Gate inside handlers.** A forgotten gate leaves the route open, and no single place shows which routes filter.
- **Compute the answer on every request, without a cache.** It's derived from every grant of the subject, on every request of every page. The cache costs a flush on each grant change instead.

## FAQ

**What happens to a subject granted part of a type, on an operation gated at `all`?**
It's refused with `403 Forbidden`. The operations of the type gated at `some` still serve them, filtered by row.

**Does this change anything for a subject whose answer is `all`?**
No. Every gate lets them in.

**Why isn't a write gated at `some`?**
A write's answer is `all` or `none` (`collection-access.md`, Section 4), so there's no `some` to let in.
