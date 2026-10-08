# Operation Gates

Details how operations on resource types are gated, for `collection-access.md` (Section 3.1).

## Background

An operation's gate can be checked inside its handler, or declared with its route. This design declares it with the route, in the same place as the row filtering that makes a `some` gate safe. _Why:_ a `some` gate is only correct while the operation really filters, and a gate declared apart from the filtering drifts from it unnoticed. Declared with the route, the gate of every operation can be read from one route table.

## Design

- **One answer.** A gate reads the subject's answer for the type and action from the access summary (`collection-access.md`, Section 4). The summary is cached per subject and set of built-in roles, and dropped whenever a grant, Scope, Role or binding changes. _Why:_ the gate then costs a cache lookup, as the object check it replaces did, and can't disagree with what the UI shows.
- **Declaring a gate.** A route names its type, its action, and its gate:
  - `some` together with row filtering, as one declaration. The route's queries run under the subject's row filters.
  - `some` for an operation that checks the subject's rules against the resource in hand. The declaration says so, and is reviewed against the handler.
  - `all` for anything else.
- **Database API.** Each table maps to a resource type, and resources' own data (changes, analyses, relationships) to their resource's type. Reads of a mapped table are gated at `some`, since the database applies the subject's row filters. Writes are gated at `all`. A table that maps to no type is checked against its object.
- **Other services.** Operations forwarded to another service, such as config-db or canary-checker, are gated at `all`. _Why:_ those services query over their own connections, so no row filter applies.
- **Shared caches.** A handler gated at `some` MUST NOT return a resource read from a cache shared between subjects, unless it checks the subject's rules against that resource first. _Why:_ a cache hit never reaches the database, so the row filters never apply to it.
- **Objects that aren't resource types.** RBAC, the Kubernetes proxy, logs, notifications and similar keep a check of their object.
- **Tests.** Every gated route is exercised by subjects whose answer for its type is `some`: a guest granted part of the type, and a member denied part of it. A resource outside their grants MUST never come back, including after another subject has read it. _Why:_ this is the only check that a `some` declaration is true, and it catches caches that serve another subject's reads.

## Other choices

- **Keep object checks, and add a deny check to each route.** Two mechanisms decide the same question, and every route must remember both. A partial grant still can't pass an object check, so guests keep their own path.
- **Gate every operation at `some`.** Operations served by other services can't filter, so they'd show resources the subject wasn't granted.
- **Gate inside handlers.** A forgotten gate leaves the route open, and no single place shows which routes filter.

## FAQ

**What happens to a member denied part of a type, on an operation gated at `all`?**
It's refused with `403 Forbidden`. The operations of the type gated at `some` still serve them.

**Does this change anything for a subject whose answer is `all`?**
No. Every gate lets them in, as the object check did.
