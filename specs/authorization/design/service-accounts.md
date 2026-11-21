# Service accounts

## Background

`service-accounts.md` makes service accounts subjects that only `serviceAccounts` selects, managed with `service-account:manage`, and signed in only with their own tokens. They can be stored in a table of their own, or as rows of `people`, the table every identity that signs in already lives in. We store them in `people`, with `type = 'service_account'`, because access tokens, `created_by` columns, the request's user and casbin subjects all refer to a person's id. A table of its own would need a second owner column on access tokens and a second kind of user on every request.

The cost of sharing `people` is that every query that picks people must keep service accounts out where they don't belong. That's met with an allowlist of person types (below).

## Storage

- A service account is a row of `people` with `type = 'service_account'`, its name in `name` and no email. Who created it isn't recorded: like GCP, AWS IAM and Kubernetes service accounts, it keeps only its creation time, `created_at`. If service accounts ever need columns of their own, they go in a table keyed by `person_id`, as `agents` is, not in `people`. Its name is unique among the rows of that type that aren't deleted, enforced by a partial unique index.
- Its tokens are rows of `access_tokens` whose `person_id` is the service account and whose `created_by` is the issuer. Their bindings are token bindings like a person's (`token-bindings.md`, Section 4), created in the token's transaction or added later, with `created_by` set to whoever wrote each one. A person is only ever soft-deleted, so a token keeps naming its issuer after they're deleted.
- Deleting a service account soft-deletes its row, and hard-deletes its tokens and their bindings, in one transaction, then reloads the policy. Each of its tokens is revoked as one token is (`design/tokens.md`): its id goes on the deny list of deleted tokens, and what's cached for it is dropped. _Why:_ a valid token is cached until it expires, so deleting its row alone would leave a cached copy working.
- The casbin subject of a service account is its person id, like a person's. `serviceAccounts` entries resolve by name to that id when the policy loads, so a binding written before the account exists takes effect on the reload its creation triggers.

## Subjects are chosen by allowlist

Every query that picks people as subjects, as owners of something, as the person a login signs in as, or as rows a person write may reach, MUST say which person types it accepts, from one table kept in one place in code:

| Who                                                   | Person types              |
| ----------------------------------------------------- | ------------------------- |
| `people` by email, `people: ["*"]`                    | humans                    |
| `people` as `<provider>:<username>` or `<provider>:*` | `federated`               |
| `serviceAccounts`                                     | `service_account`         |
| Members of a team                                     | humans, `federated`       |
| A login or session of Mission Control's own authentication (Kratos, Clerk, basic, embedded OIDC) | humans |
| Reached by a person operation (`roles.md`, Section 2.3) | humans, `federated`       |
| Owner of a token limited by its bindings              | humans, `service_account` |
| Owner of an unlimited token                           | `agent`                   |

Humans are the rows Mission Control's own authentication (Kratos, Clerk, basic) creates, which have a `NULL` type, less the `System` row, which has neither a type nor an email. A type missing from a row is never selected by it, including types added later.

_Why not a denylist:_ a query that excludes the types it knows about selects every type added after it was written. One written before `service_account` existed would give every service account the Roles bound to `people: ["*"]`, and let teams take them as members. With an allowlist, a new type selects nothing until a row of this table names it.

A person's `type` MUST NOT change once the row is written, whatever the type, enforced by the database. _Why:_ each check reads the type once, and a row that changed type afterwards would keep what it was let into.

### Logins

Every lookup that turns a login into a person, e.g. the htpasswd and embedded OIDC lookup by name or email, and session restoration, uses the login row of the allowlist, so a username equal to a service account's name finds no one. Access-token authentication is a separate path and looks the token's owner up by id.

### Team membership

A team's members are resolved through the allowlist where a membership becomes a grant: when one is applied to the policy, and when the policy loads its stored groupings. A membership of any other type of person is skipped there, so it grants nothing, however it was written. Whatever else reads a team's members, e.g. to list them, resolves them the same way.

Writing such a membership is refused as well. `team_members` is read-only through the database API, and members are added and removed only by the routes under "Teams" below, which find each person through the team-membership row of the allowlist, so one that names a service account fails as not found. The other writers are Mission Control's own: team mappers, accepted invites, Team custom resources and the shipped `admins` team. Each finds its people through the same row, e.g. a Team resource that lists a service account's id skips it. _Why not a rule in the database:_ with every writer in Mission Control, the allowlist in code covers them all, and a second copy of it in duty's schema would only have to be kept in step.

### Person writes

`people` and Kratos's `identities` are read-only through the database API. `person:manage` writes a person only through the person operations (`roles.md`, Section 2.3), by the routes under "People" below. Each writes a fixed set of fields, never `type`, and finds its row through the person-operation row of the allowlist, so one that names a service account fails as not found.

_Why not `/db/people`:_ a table write is gated by one action for every column and every row, so it reaches whatever a release adds to the table until something forbids it, which is a denylist again. An operation that names its fields reaches nothing else. Scopes, Roles and RoleBindings are written only through their API for the same reason: what's written has to be validated.

_Why `identities` too:_ a change to an identity is copied into the row of `people` that has the identity's id, whatever that row's type. A write that set an identity's id to a service account's would rename the service account, and deleting that identity would delete it without revoking its tokens.

### Reads

Rows of `people` are read through the database API with `read` on `person`, service accounts' included: a name and a type, never a credential. _Why not filtered out:_ a record names who acted by their row of `people`, so a reader who couldn't read a service account's row would see a run with no creator. Listing a service account's tokens, or the accounts with them, goes through the routes below.

The subjects offered for an access review include service accounts, by name, as a kind of their own. A review or search run as one is checked as its person id, like a person's.

## What duty provides

The schema of `people`, `team_members` and `access_tokens`, and the views over them, belongs to duty, not to Mission Control. So these parts of the design are duty's, and service accounts need a duty version that has them:

- the partial unique index on service account names (Storage),
- the rule that a person's `type` never changes,
- service accounts among the subjects offered for an access review (Reads).

The allowlist, the routes and the policy are Mission Control's.

## API

The `/service-accounts` routes all take `service-account:manage`, and refuse a request made with an access token or by a `federated` person, whatever their grants. The kubeconfig download for a service account refuses them too.

| Route                                                 | Does                                                        |
| ----------------------------------------------------- | ----------------------------------------------------------- |
| `GET /service-accounts`                               | Lists service accounts                                      |
| `POST /service-accounts`                              | Creates one: `name`                                         |
| `GET /service-accounts/:name`                         | Reads one, with whether it holds `kubernetes-proxy:read` or `kubernetes-proxy:write`, and, for a caller with `read` on `roleBinding`, the RoleBindings that name it |
| `DELETE /service-accounts/:name`                      | Deletes it and its tokens                                   |
| `GET /service-accounts/:name/tokens`                  | Lists its tokens, with their Roles, never the token         |
| `POST /service-accounts/:name/tokens`                 | Issues a token: `name`, `expiry`, `autoRenew`, `roles`      |
| `DELETE /service-accounts/:name/tokens/:id`           | Revokes a token                                             |
| `POST /service-accounts/:name/tokens/:id/roles`       | Adds a Role to a token: `namespace`, `name` (`design/tokens.md`) |
| `DELETE /service-accounts/:name/tokens/:id/roles/:namespace/:name` | Removes a Role from a token (`design/tokens.md`) |

Issuing a token shares the code path of a person's token: one transaction creating the token and its bindings, then a policy reload before the response, so it's usable as soon as it's returned (`design/tokens.md`).

### Kubeconfig

`GET /kubeconfig` takes an optional `serviceAccount` query parameter. Without it, the kubeconfig is the caller's own (`roles.md`, Section 2.3). With it, the request takes `service-account:manage` instead of `kubernetes-proxy:read` or `kubernetes-proxy:write`, and the kubeconfig carries a new token of the service account bound to `mission-control-builtin/kubernetes-proxy`. The proxy then checks the service account's own grants on every request.

With `serviceAccount`, the request MAY also carry the `name`, `expiry` and `autoRenew` of the token, as when one is issued. Without them the token is named after the time of the download, expires after the default expiry and doesn't auto-renew.

_Why a parameter, not a route under `/service-accounts`:_ the kubeconfig template, tenant naming and download headers stay in one handler.

### Teams

These routes replace writes to `/db/team_members`. Both take `update` on `team`, as changing a team's members does today (`design/bootstrap.md`).

| Route                                                 | Does                                                        |
| ----------------------------------------------------- | ----------------------------------------------------------- |
| `POST /teams/:id/members`                             | Adds people to the team: `personIds`, all or none. A person the team-membership row of the allowlist doesn't reach fails the request with `404`, and one already a member with `409` |
| `DELETE /teams/:id/members/:personId`                 | Removes a direct member. A member a team mapper added is removed by the next sign-in's mapping, not here |

_Why a list on add:_ the Team page adds several people at once, and adding only some of them would leave it guessing which.

### People

These routes are the person operations (`roles.md`, Section 2.3): the only way a holder of `person:manage` writes a person, since `/db/people` and `/db/identities` are read-only. All take `person:manage`, and none takes a `type`.

| Route                                                 | Does                                                        |
| ----------------------------------------------------- | ----------------------------------------------------------- |
| `POST /people`                                        | Creates a person: `name`, `email`. Only under basic authentication, where signing in creates no one and a login is matched to a person by name or email. Refused elsewhere: Kratos creates a person when an invite is accepted, and Clerk on their first sign-in |
| `POST /people/update`                                 | Changes a person's name or email: through their Kratos identity when they have one, on the row otherwise |
| `DELETE /people/:id`                                  | Deletes a person: through their Kratos identity when they have one, soft-deleting the row otherwise |
| `POST /auth/:id/properties`                           | Changes a person's properties                               |
| `POST /auth/invite_user`, `POST /auth/:id/update_state` | Invites a person, and disables or re-enables one, under Kratos |

_Why a route to create one:_ under basic authentication signing in doesn't create a person, and nothing else does. _Why the other routes outside Kratos:_ they're the only other way a name or email is changed, or a person deleted, there.

## UI

- The avatar menu's **Download kubeconfig** stays as it is, and is always for the signed-in person.
- The **Users** page lists the people a person operation reaches, so never a service account.
- Every person picker, e.g. a RoleBinding's `people`, a team's members, notification recipients and playbook `people` parameters, offers only the types its field accepts, so never a service account. RoleBinding subjects get a separate service account picker.
- **Settings → Service Accounts**, shown to subjects with `service-account:manage`:
  - A list with **Create**, taking a name, and each account's creation time.
  - An account page listing the RoleBindings that name it, to those who may read RoleBindings, read-only, linking to the RoleBindings page to edit them.
  - A **Tokens** tab to issue a token (name, expiry, auto-renew, Roles), shown once, to add or remove a token's Roles, and to revoke tokens.
  - **Download kubeconfig**, which issues a proxy token for the account, with the expiry and auto-renew chosen there. The page warns when the account holds neither `kubernetes-proxy:read` nor `kubernetes-proxy:write`, since such a kubeconfig is refused on every request. It reads that from the account, so it needs no `access:review`.

The UI never acts as a service account: an admin, signed in as themselves, manages the account and its credentials, as GCP's and AWS's consoles do for service account keys.

## Options considered

- **A table of its own.** Keeps `people` to humans, but every reference to a token's owner or a request's user would have to accept either table. Rejected for the cost, since the allowlist already keeps service accounts out of what selects humans.
- **A person with a flag, e.g. `is_service_account`.** Equivalent to a type, but a second column describing the same thing as `type`. Rejected.
- **Refusing a service account's team membership only where it's written.** It would be all that stands between a stray row and a team's Roles. Rejected as the only check: the policy resolves members through the allowlist too.
- **A rule in the database on `team_members`.** It would refuse a wrong membership whoever wrote it, but `/db/team_members` was the only writer outside Mission Control's code, and closing it leaves none. Rejected as a second copy of the allowlist, in duty's schema.
- **Guarding `/db/people` writes instead of closing them.** A guard has to work out which rows and fields a database API request touches, which it can only do for requests it restricts to ids and JSON bodies, as `/db/teams` is for the shipped team (`design/bootstrap.md`). Rejected: every column added to `people` would be writable until the guard learnt of it.
- **Separate actions to create accounts and to issue their tokens.** Rejected in `service-accounts.md`, Section 3: whoever may create an account could issue tokens for it once an admin granted it Roles.
- **Tokens without Roles, like an agent's.** Simpler, but a kubeconfig, which is written to a file that's easily shared, would carry everything the account may do. Rejected so each token carries only what it's for.
