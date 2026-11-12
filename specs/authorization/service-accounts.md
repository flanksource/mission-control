# Service Accounts

A service account is a subject that isn't a person: a bot, a CI job or an integration that calls Mission Control on its own behalf. It signs in only with access tokens, and is granted Roles by RoleBindings that name it.

```yaml
apiVersion: mission-control.flanksource.com/v1
kind: RoleBinding
metadata:
  name: ci-deployer
  namespace: platform
spec:
  role: deployer
  subjects:
    serviceAccounts:
      - github-actions
```

The service account `github-actions` gets `deployer`, and its tokens can do what `deployer` allows, within the Roles each token is created with.

_Why:_ automation that borrows a person's token stops working when that person leaves or loses access, and does whatever that person may do. A subject of its own keeps working whoever comes and goes, and can only do what is granted to it by name.

## 1. What a service account is

- **A subject, not a person.** A service account is checked like any other subject (`overview.md`, "Access"). It's selected only by `serviceAccounts` in a RoleBinding (`rolebindings.md`, Section 2.7). `people`, by any entry, `*` included, never selects it, and it can't be a member of a team. _Why:_ `people: ["*"]` and teams are how organisations grant their people, and a service account would otherwise pick up Roles meant for humans. Naming it in every binding that grants it keeps what it may do readable from those bindings alone.
- **Deny bindings apply.** A deny RoleBinding that names a service account refuses it the Role, like any subject (`rolebindings.md`, Section 5). Only one that names it does: a deny on `people: ["*"]` or on a team never selects it (`rolebindings.md`, Section 5.6).
- **No session.** A service account MUST NOT sign in interactively, through Kratos, Clerk, basic authentication or any other login. A login whose username or email matches a service account's name never signs in as the service account. Its tokens are its only credential. _Why:_ a session isn't limited by a token's Roles and may manage service accounts, so a login that reached a service account would lift every limit its tokens carry.
- **Recorded as itself.** Actions taken with a service account's token are recorded as the service account, e.g. as the creator of a playbook run. Its name and description are read with `read` on `person`, like those of anyone a record names. _Why:_ whoever may read a run must be able to see which service account started it. Nothing else about it is read that way: its tokens take `service-account:manage` (Section 3).
- **No owner.** A service account doesn't belong to whoever created it, and its creator gets nothing through it. Who created it is recorded for audit only.

## 2. Identity

A service account is identified by its name, which is unique among service accounts.

- A name MUST be a valid DNS-1123 label: at most 63 lowercase letters, digits and `-`, starting and ending with a letter or digit. _Why:_ it's written in RoleBindings, URLs and CLI flags, so it needs no quoting or escaping anywhere.
- Service accounts have no namespace. _Why:_ teams and agents, the other subjects named by a single name, have none either, and a namespace limits nothing (`overview.md`, "Namespaces do not limit access").
- A service account MAY have a description. Its name can't be changed.
- Its name is unrelated to any person's: a service account named `alice` and a person whose email is `alice@example.com` are different subjects.

## 3. Managing service accounts

Everything about service accounts takes one action without a resource, `service-account:manage` (`roles.md`, Section 2.3):

- listing service accounts and their tokens,
- creating a service account, changing its description and deleting it,
- issuing and revoking its tokens, and downloading a kubeconfig for it (Section 4.1).

The one thing it doesn't gate is a service account's name and description, which `read` on `person` shows wherever the account is named (Section 1).

Giving a service account Roles is writing a RoleBinding that names it, which takes `create` or `update` on `roleBinding` like any other binding (`overview.md`, "Who may manage these resources").

- **As privileged as every service account.** Whoever may issue a service account's token may act as it, so `service-account:manage` reaches every grant of every service account, and is only for subjects trusted with all of them. It grants no Roles: only a subject who may write RoleBindings can give a service account any access, and that is already as privileged as `admin` (`overview.md`, "Who may manage these resources"). So a subject with `service-account:manage` alone can create a service account, which can do nothing unless a binding already names it. A binding that names an account that doesn't exist, written ahead of it or left behind by a deleted one (Section 5), is taken by whoever creates an account of that name.
- **Why one action.** Creating a service account grants nothing on its own: a new one has no Roles and no token. Issuing its token is what gives power, since it lets the holder act as the account. If creating and issuing were separate actions, someone allowed to create an account could later issue tokens for it after an admin had granted it Roles, and gain those Roles. Deleting an account and revoking its tokens only take access away. None of these is safer to grant than the others, so splitting them would only suggest that one is. _Why not an action per service account:_ actions without a resource can't pick some service accounts. Letting a team manage its own needs a Scope over service accounts and a check that a binding grants no more than its author holds, which RoleBindings don't have yet (Section 6).
- **Not a resource type.** Service accounts are identities, managed through one named action like accounts are through `person:manage`. No Scope selects them, and `read`, `create`, `update` and `delete` don't apply to them, apart from `read` on `person` showing their name and description (Section 1).
- **Not a custom resource.** There's no `ServiceAccount` CRD. _Why:_ a service account is useless without a token, and tokens can only be created through the API, so declaring the account in Git would only move half of it there. Kubernetes already has a kind named `ServiceAccount`, so a CRD of the same kind would be ambiguous in `kubectl`. RoleBindings from Git can still name a service account (Section 5).
- **Not as a person.** Service accounts MUST NOT be created, changed or deleted through the ways people are managed, e.g. with `person:manage`, and MUST NOT be added to a team. The Users page doesn't list them. Nothing changes a person into a service account, or a service account into a person. _Why:_ `service-account:manage` is the only boundary around service accounts. A person write that reached one could rename it into the name another account's bindings select, or turn it into a person that `people: ["*"]` grants.
- **Not by users of an external identity provider.** A user of an external identity provider MUST NOT do anything `service-account:manage` allows, even when a binding grants it to them. _Why:_ the same reason they can't create tokens of their own (`access-tokens.md`, Section 2): a service account's token would let them act long after the application that signed them in stops vouching for them.
- **Not through a token.** A request made with an access token MUST NOT do anything `service-account:manage` allows, whatever its bindings. _Why:_ tokens don't manage tokens (`access-tokens.md`, Section 2): a token could otherwise mint a token for any service account, and a leaked one could keep itself alive by minting its own successor. So service accounts and their tokens are always managed by a person who is signed in: a token without auto-renew is replaced by hand before it expires, and a service account can't use `service-account:manage` even when a binding grants it.

## 4. Tokens

A service account's token is an access token (`access-tokens.md`) that signs in as the service account.

- **Created with Roles.** A token MUST be created with at least one Role, which limits it like a person's token: it may do what its service account may do, and only what a rule of one of its Roles also matches (`limiting-permissions.md`). The Roles become RoleBindings that select the token, written by whoever issues it (`token-bindings.md`). A token meant to do everything its service account may is bound to `admin` and `plugin-admin`. _Why:_ a token's reach is chosen when it's handed out, never everything by default. One service account can then serve several clients, each with a token that carries only what it's for, e.g. a kubeconfig that reaches only the proxy.
- **Outlives its issuer.** A token's access comes from its service account's bindings and its own Roles, never from who issued it. It keeps working when its issuer loses access or is deleted.
- **Expiry.** A token expires after the expiry given when it's created, or after `access_token.default_expiry`, 90 days by default, when none is given. It MAY be created with auto-renew, which extends it as `access-tokens.md`, Section 4 says. A token without an expiry can't be created.
- **Names are unique per service account.** A service account can't have two tokens of the same name.
- **Usable at once**, and shown once, as for every token (`access-tokens.md`, Sections 2 and 5).
- **Revocation.** Revoking a token deletes it and its bindings at once, and the next request made with it is refused with `401 Unauthorized`. Expired tokens are deleted on a schedule.
- **Listing** a service account's tokens shows each token's name, its Roles, who issued it, and when it was created and expires, never the token.

### 4.1 Kubeconfig

A kubeconfig for the Kubernetes proxy can be downloaded for a service account, as well as for oneself (`roles.md`, Section 2.3). Downloading one for a service account takes `service-account:manage`. Its credential is a new token of the service account bound to the shipped `kubernetes-proxy` Role, so it may do in the cluster what the service account's own `kubernetes-proxy:read` or `kubernetes-proxy:write` grants, and nothing else. A service account with neither gets a kubeconfig that's refused on every request.

The token is one of the account's tokens like any other (Section 4). Whoever downloads the kubeconfig MAY give it a name, an expiry and auto-renew, with the same defaults, and it's listed and revoked with the account's other tokens.

_Why:_ a kubeconfig in CI or a tool shouldn't stop working when the person who downloaded it leaves, and shouldn't carry that person's access to the cluster. _Why its lifetime is chosen:_ it's meant to keep working unattended, which the default expiry without auto-renew doesn't allow.

## 5. Lifecycle

- **Created with no access.** A new service account can do nothing until a RoleBinding names it.
- **Bound by name.** A RoleBinding names a service account by name, so it MAY name one that doesn't exist yet, e.g. from Git before the account is created. It selects no one until the account exists, and the account gets the Role on creation, at once.
- **Deleted.** Deleting a service account deletes all of its tokens and their bindings in the same transaction, so each is refused on its next request with `401 Unauthorized`. The RoleBindings that name it are left alone and select no one. To retire the name for good, delete them too: they select whichever account is created under it next. Records of what it did, e.g. as the creator of a run, still name it.
- **Recreated.** A service account created again under a deleted one's name is selected by the bindings that name it, like a person who signs up with a bound email. None of the deleted account's tokens comes back. _Why:_ bindings follow names, so a recreated account has the access its bindings describe. Tokens are credentials of the account that was deleted, and revoking them was the point of deleting it.

## 6. Not covered yet

- **Delegated management. TODO.** Letting a team create service accounts and issue their tokens without being trusted as admins needs `service-account:manage` to pick some service accounts, and a check that no one can bind a service account to more than they hold themselves. Kubernetes checks the second when a RoleBinding is written. Both are left for later, with delegated administration of RoleBindings (`overview.md`, "Not covered yet").
- **Denying every service account. TODO.** A deny binding can't select every service account, present and future, since `serviceAccounts` takes no `*` (`rolebindings.md`, Section 5.6). Unlike a grant, a deny on `*` would hand nothing to an account created later, so `*` could be accepted on deny bindings alone.
- **Federated credentials. TODO.** A CI system that issues its own signed tokens, e.g. GitHub Actions OIDC, could sign in as a service account without a stored token. That's a later extension of external identity providers.

The design is in `design/service-accounts.md`.
