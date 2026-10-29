# Access Tokens

An access token is a long-lived credential for clients that aren't a browser: scripts, CI, MCP clients and agents. It's stored by Mission Control, so it can be revoked, unlike the short-lived signed tokens Mission Control mints for its own use after a request is authenticated (`design/tokens.md`).

## 1. A credential, not an identity

A request made with a person's token is checked as that person, and RoleBindings grant the person, never the token. Actions taken with it are recorded as the person's. What a token may do is limited by the RoleBindings that select it (`token-bindings.md`, `limiting-permissions.md`).

_Why:_ a token is a way to sign in as its owner from somewhere a browser session can't reach. Granting it Roles of its own would make it a second identity whose access drifts from its owner's.

## 2. People's tokens

A person MAY create, list and delete their own tokens without any grant, but MUST NOT do so through a token. No person can create, change or delete a token for another person, and no action grants it.

- **Created with Roles.** A person's token MUST be created with at least one Role, however it's created: through the UI, the API, the kubeconfig download (`roles.md`, Section 2.3) or the `auth token` CLI command. Each Role becomes a RoleBinding that selects the token, created with it (`token-bindings.md`). A token's Roles can't be changed afterwards; to change them, its owner creates a new token. A token meant to do everything its owner may is bound to the shipped `admin` and `plugin-admin` Roles. _Why:_ what a token may do is chosen when it's created, never everything by default.
- **A token no binding selects can do nothing.** It still signs in as its owner, but every check made with it fails. Only what needs no grant, e.g. properties, is reachable with it. _Why not refuse it:_ a binding's Role can be deleted or become invalid, and come back, which changes the token's limit, not whether the credential is valid.
- **Tokens don't manage tokens.** A request made with a token MUST NOT create, list or delete tokens, whatever its bindings. _Why:_ otherwise a limited token could create a token without its limit, and a leaked token would reveal its owner's other tokens and their Roles.
- **Not for users of an external identity provider.** They're checked as the bindings their claims match, not as a person (`external-identity-providers.md`), so a token signing in as their person would give them grants they don't have.
- **Names are unique per owner.** A person can't have two tokens of the same name.
- **Usable at once.** A token created through the API or the UI MUST be usable as soon as it's returned. One created with the `auth token` CLI command, which runs apart from Mission Control's server, MAY take up to a few seconds to become usable. _Why:_ a client is configured with a token straight after creating it, so a delay there would fail its first request.

_Why no grant:_ a token adds no access its owner doesn't already have, so there's nothing for a Role to decide.

## 3. Agents' tokens

An agent's token is created with the agent, by a subject with `create` on `agent`, and handed to them (`identities.md`, Section 5). Listing an agent's tokens takes `read` on `agent`; it shows when each was created and expires, never the token. Rotating or revoking an agent's token takes `update` on `agent`.

An agent's token isn't limited on requests made as its agent, so no RoleBinding selects it, and it's created with no Roles.

_Why:_ an agent only pushes its own data and fetches what's assigned to it, which the token alone allows (`identities.md`, Section 5).

## 4. Lifetime

- **Expiry.** A person's token expires after the expiry given when it's created. Without one, a token created through the API expires after 90 days, configurable with `access_token.default_expiry`, and one created with the `auth token` CLI command after 4 hours. An agent's token expires after 365 days.
- **Auto-renew.** A token created with auto-renew has its expiry extended by 30 days when it's used within its last 15 days. _Why:_ a token in steady use, e.g. by CI, keeps working, while an abandoned one still expires.
- **Revocation.** Deleting a token revokes it at once: the next request made with it is refused with `401 Unauthorized`, and the RoleBindings that select it are deleted with it. Expired tokens are deleted on a schedule.

## 5. Using a token

A token is sent as `Authorization: Bearer <token>`, or with basic authentication as the user `token` and the token as the password.

The token is shown once, when it's created. Mission Control stores only a hash of it, so it can't be shown again or recovered.
