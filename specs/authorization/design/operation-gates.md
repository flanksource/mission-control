# Operation Gates

Details how operations on resource types are gated, for `collection-access.md` (Section 3.1).

## Background

An operation's gate can be checked inside its handler, or declared with its route. This design declares it with the route, in the same place as the row filtering that makes a `some` gate safe. _Why:_ a `some` gate is only correct while the operation really filters, and a gate declared apart from the filtering drifts from it unnoticed. Declared with the route, the gate of every operation can be read from one route table.

## Design

- **One answer.** A gate reads the subject's answer for the type and action from the access summary (`collection-access.md`, Section 4).
- **Cache.** Answers are cached per subject, built-in roles, impersonated Scopes and, for a user of an external identity provider, the set of `oidc` subjects their token matches. Every cached answer is dropped when a Permission, PermissionGroup, Scope, Role, RoleBinding, team membership, built-in role, agent or a resource that's a subject (a playbook, notification, topology, scraper or canary) changes. _Why:_ the key covers what's read per request; the events cover what's stored. A token's other claims, such as its expiry, aren't in the key, so a new token that matches the same bindings reuses the answer. Dropping everything on any change is coarse, but these changes are rare, and a partial drop can miss a subject.
- **Declaring a gate.** A route or MCP tool names its type, its action, and its gate:
  - `some` together with row filtering, as one declaration. The operation's queries run under the subject's row filters.
  - `some` for an operation that checks the subject's rules against the resource in hand. The declaration says so, and is reviewed against the handler.
  - `all` for anything else.
- **Row filters.** A subject's row filter for a type is built whenever their `read` answer for it is `some`, members included. It admits the rows of their grants' Scopes, and leaves out the rows of their denies' Scopes. A subject answered `all` on every type gets no filter. A deny the filter can't express never reaches it: it makes the answer `none` (`collection-access.md`, Section 2.2).
- **Database API.** Each table maps to a resource type, and resources' own data to their resource's type. A read of a table is gated at `some` only once the table's row filtering is in place and exercised by the tests below. Any other table of a resource type is gated at `all`. Views and functions are gated at `some` only when they run as the caller, so the row filters apply to them. Writes are gated at `all`. A table that maps to no type is checked against its object. _Why:_ a table is safe at `some` only once its filtering is shown to work; until then, `all` refuses rather than leaks.
- **Other services.** Operations forwarded to another service, such as config-db or canary-checker, are gated at `all`. _Why:_ those services query over their own connections, so no row filter applies.
- **Shared caches.** A handler gated at `some` MUST NOT return a resource read from a cache shared between subjects, unless it checks the subject's rules against that resource first. _Why:_ a cache hit never reaches the database, so the row filters never apply to it.
- **Agent pushes.** What agents push through their upstream connection is checked against `agent-push`, whatever it writes. A topology pushed directly is a component `update`, gated at `all`.
- **Objects that aren't resource types.** RBAC, the Kubernetes proxy, logs, notifications and similar keep a check of their object.
- **Tests.** Every gated route and MCP tool is exercised by subjects whose answer for its type is `some`: a guest granted part of the type, and a member denied part of it. A resource outside their grants MUST never come back, including after another subject has read it. _Why:_ this is the only check that a `some` declaration is true, and it catches caches that serve another subject's reads.

## Other choices

- **Keep object checks, and add a deny check to each route.** Two mechanisms decide the same question, and every route must remember both. A partial grant still can't pass an object check, so guests keep their own path.
- **Gate every operation at `some`.** Operations served by other services can't filter, so they'd show resources the subject wasn't granted.
- **Gate inside handlers.** A forgotten gate leaves the route open, and no single place shows which routes filter.
- **Compute the answer on every request, without a cache.** It's derived from every grant of the subject, on every request of every page. The cache costs a flush on each grant change instead.

## FAQ

**What happens to a member denied part of a type, on an operation gated at `all`?**
It's refused with `403 Forbidden`. The operations of the type gated at `some` still serve them, filtered by row.

**Does this change anything for a subject whose answer is `all`?**
No. Every gate lets them in, as the object check did.

**Does a deny on part of a write lock the subject out of every write of the type?**
Yes. Writes are gated at `all`, and the deny makes the answer `some`. Narrow the deny to whole types, or remove it.
