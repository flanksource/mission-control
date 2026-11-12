# Service accounts

## Background

`service-accounts.md` makes service accounts subjects that only `serviceAccounts` selects, managed with `service-account:manage`, and signed in only with their own tokens. They can be stored in a table of their own, or as rows of `people`, the table every identity that signs in already lives in. We store them in `people`, with `type = 'service_account'`, because access tokens, `created_by` columns, the request's user and casbin subjects all refer to a person's id. A table of its own would need a second owner column on access tokens and a second kind of user on every request.

The cost of sharing `people` is that every query that picks people must keep service accounts out where they don't belong. That's met with an allowlist of person types (below), rather than the denylists in place today.

## Storage

- A service account is a row of `people` with `type = 'service_account'`, its name in `name`, no email, and its description in `properties`. Its name is unique among the rows of that type that aren't deleted, enforced by a partial unique index.
- Its tokens are rows of `access_tokens` whose `person_id` is the service account and whose `created_by` is the issuer. Their bindings are token bindings like a person's (`token-bindings.md`, Section 4), created in the token's transaction.
- Deleting a service account soft-deletes its row, and hard-deletes its tokens and their bindings, in one transaction, then reloads the policy.
- The casbin subject of a service account is its person id, like a person's. `serviceAccounts` entries resolve by name to that id when the policy loads, so a binding written before the account exists takes effect on the reload its creation triggers.

## Subjects are chosen by allowlist

Every query that picks people as subjects, or as owners of something, MUST say which person types it accepts, from one table kept in one place in code:

| Who                                                   | Person types              |
| ----------------------------------------------------- | ------------------------- |
| `people` by email, `people: ["*"]`                    | humans                    |
| `people` as `<provider>:<username>` or `<provider>:*` | `federated`               |
| `serviceAccounts`                                     | `service_account`         |
| Members of a team                                     | humans, `federated`       |
| Owner of a token limited by its bindings              | humans, `service_account` |
| Owner of an unlimited token                           | `agent`                   |

Humans are the rows Mission Control's own authentication (Kratos, Clerk, basic) creates, which have a `NULL` type, less the `System` row, which has neither a type nor an email. A type missing from a row is never selected by it, including types added later.

_Why not a denylist:_ a query that excludes the types it knows about selects every type added after it was written. Adding `service_account` to today's queries would have given every service account the Roles bound to `people: ["*"]`, and let teams take them as members. With an allowlist, a new type selects nothing until a row of this table names it.

The allowlist replaces the type filters spread across the RoleBinding subject resolver, team membership and token ownership. It changes no behaviour for existing types, so it lands before service accounts.

## API

All routes take `service-account:manage`, and refuse a request made with an access token.

| Route                                                 | Does                                                        |
| ----------------------------------------------------- | ----------------------------------------------------------- |
| `GET /service-accounts`                               | Lists service accounts                                      |
| `POST /service-accounts`                              | Creates one: `name`, `description`                          |
| `GET /service-accounts/:name`                         | Reads one, with the RoleBindings that name it               |
| `PATCH /service-accounts/:name`                       | Changes its description                                     |
| `DELETE /service-accounts/:name`                      | Deletes it and its tokens                                   |
| `GET /service-accounts/:name/tokens`                  | Lists its tokens, with their Roles, never the token         |
| `POST /service-accounts/:name/tokens`                 | Issues a token: `name`, `expiry`, `autoRenew`, `roles`      |
| `DELETE /service-accounts/:name/tokens/:id`           | Revokes a token                                             |

Issuing a token shares the code path of a person's token: one transaction creating the token and its bindings, then a policy reload before the response, so it's usable as soon as it's returned (`design/tokens.md`).

### Kubeconfig

`GET /kubeconfig` takes an optional `serviceAccount` query parameter. Without it, it behaves as today. With it, the request takes `service-account:manage` instead of `kubernetes-proxy:read` or `kubernetes-proxy:write`, and the kubeconfig carries a new token of the service account bound to `mission-control-builtin/kubernetes-proxy`. The proxy then checks the service account's own grants on every request.

_Why a parameter, not a route under `/service-accounts`:_ the kubeconfig template, tenant naming and download headers stay in one handler.

## CLI

- `service-account list|create|delete <name>`
- `service-account token create <name> --role <namespace/name> [--expiry] [--auto-renew]`, `service-account token list|revoke`
- `--service-account <name>` on the kubeconfig download

## UI

- The avatar menu's **Download kubeconfig** stays as it is, and is always for the signed-in person.
- **Settings → Service Accounts**, shown to subjects with `service-account:manage`:
  - A list with **Create**, taking a name and description.
  - An account page listing the RoleBindings that name it, read-only, linking to the RoleBindings page to edit them.
  - A **Tokens** tab to issue a token (name, expiry, auto-renew, Roles), shown once, and to revoke tokens.
  - **Download kubeconfig**, which issues a proxy token for the account. The page warns when the account holds neither `kubernetes-proxy:read` nor `kubernetes-proxy:write`, since such a kubeconfig is refused on every request.

The UI never acts as a service account: an admin, signed in as themselves, manages the account and its credentials, as GCP's and AWS's consoles do for service account keys.

## Options considered

- **A table of its own.** Keeps `people` to humans, but every reference to a token's owner or a request's user would have to accept either table. Rejected for the cost, since the allowlist already keeps service accounts out of what selects humans.
- **A person with a flag, e.g. `is_service_account`.** Equivalent to a type, but a second column describing the same thing as `type`. Rejected.
- **Separate actions to create accounts and to issue their tokens.** Rejected in `service-accounts.md`, Section 3: whoever may create an account could issue tokens for it once an admin granted it Roles.
- **Tokens without Roles, like an agent's.** Simpler, but a kubeconfig, which is written to a file that's easily shared, would carry everything the account may do. Rejected so each token carries only what it's for.
