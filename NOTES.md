# Scrapers as a Scope type: what duty still needs

Mission Control's side of flanksource/duty#2111 (duty v1.0.1399, i.e. 82b0565) filters scraper listings by row.
Single-resource (Casbin) checks on a scraper aren't possible yet, and Mission Control doesn't fake them. Today:

- A Role rule over a Scope that selects scrapers compiles to its usual `*` policy, whose condition reads
  `r.obj.Membership`. No request carries a scraper, so the condition never holds for one: it fails closed.
- Whole-type rules (`scraper: {name: "*"}`) also compile to the `scrapers` object, which `/db/config_scrapers`
  accepts. That's how `create`, `update` and `delete` on scrapers are enforced.
- A Permission naming a Scope gets no condition for the Scope's scraper targets
  (`adapter.PermissionScopeConditions`). Its other types still match, and its scraper targets still filter
  listings through the claim.

## What duty needs

1. **`models.ABACAttribute.Scraper`** (`models.ConfigScraper`), so a check can carry a scraper.
2. **`rbac.requestResources`**: `add(policy.ResourceScraper, attr.Scraper.ID)`, so `MembershipRefs` and
   `withMembership` read the scraper's `scope_members` rows, and `Fits`/`Resource` count it as the primary resource.
3. **`models.ABACMembership.Scraper []any`** and a `policy.ResourceScraper` case in `rbac.membershipOf`, so the
   Scopes the scraper is in are filled in.
4. **`rbac.ScopeCondition`**: map `policy.ResourceScraper` to `"Scraper"`, so a Permission naming a Scope can match
   a scraper (`'scope:<id>' in r.obj.Membership.Scraper`). Once it's there, remove the scraper skip in
   `rbac/adapter/scope_expansion.go`.
5. **Optional: an RBAC object of its own for scrapers in duty** (`policy.ObjectScrapers = "scrapers"`), with
   `config_scrapers` and `config_scrapers_with_status` mapped to it in `rbac/objects.go`. Today `config_scrapers` is
   filed under `database.config_scrapers`, which `logging_backends` shares, and the view under `database.monitor`.
   So Mission Control accepts its own `scrapers` object on those two tables (`rbac.scraperGrants`). With the
   mapping in duty, that shim can go, but the built-in roles' grants on `database.config_scrapers` must then move
   to the new object.

After 1 to 4, Mission Control's handlers that act on one scraper (e.g. run now, delete) can check
`HasPermission(ctx, subject, &models.ABACAttribute{Scraper: s}, action)`, so partial Scopes work for `read` on a
single scraper. `create`, `update` and `delete` stay whole-type only (`roles.md` §2), as they are for every other
type.
