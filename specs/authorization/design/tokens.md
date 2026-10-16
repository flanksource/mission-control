# Tokens in Mission Control

## Background

`access-tokens.md` and `token-bindings.md` say what an access token may do and who manages it. This design says how tokens are built and checked. A credential for clients that aren't a browser can be an opaque token stored by Mission Control, or a self-contained signed token. Access tokens are opaque and stored, because they're long-lived and must be revocable at once: deleting the row revokes them, which a signed token can't offer before it expires. Short-lived signed tokens are minted after authentication for the subsystems that need a verifiable claim without a database lookup.

At a high level Mission Control has two credential families:

1. **Access tokens** — long-lived, opaque, DB-backed, revocable credentials.
2. **Signed tokens** — short-lived JWTs minted for a specific subsystem or protocol.

Access tokens can be used to authenticate a request. After authentication, Mission Control may mint an internal signed token for downstream use.

## Mission Control access tokens

Long-lived access tokens are opaque Mission Control credentials, stored hashed in the `access_tokens` table.

They are different from the short-lived signed JWTs described below: access tokens are revocable database-backed credentials intended for non-browser clients and delegated identities.

### Token format

The returned token has this v1 format:

```text
password.salt.timeCost.memoryCost.parallelism
```

New tokens use a v2 format that binds extra token material into the hashed secret:

```text
password.salt.timeCost.memoryCost.parallelism.binding
```

Only the Argon2id hash is stored in the database. For v2 tokens, the hash input includes both the password and binding, so replacing the appended binding invalidates the token. Agent tokens use `base64url(jwk-json)` as the binding; other tokens use a generated random binding. The raw token is shown once to the caller.

### Issuance flows

| Flow                 | Created by                         | Owner              | Lifetime                                                                 | Use case                                                                             |
| -------------------- | ---------------------------------- | ------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------------------ |
| Agent token          | Creating or rotating an agent      | The agent          | 365d                                                                     | Primary long-lived credential for agents. v2 tokens append the public JWK agents use to verify upstream tunnel JWTs. |
| Person's token       | `POST /auth/create_token`          | The caller         | Default 90d via `access_token.default_expiry`; request can supply expiry | Scripts, CI and MCP clients, limited by the Roles they're created with.               |
| CLI-created token    | The `auth token` command           | The named person   | Default 4h unless `--expiry` supplied                                    | Operator-created token.                                                              |

### Delegated token model

A token is a credential: it signs in as its owner, and the RoleBindings that select it (`token-bindings.md`) only limit what the owner may do on requests made with it.

For `/auth/create_token`:

1. An access token is created with `person_id` and `created_by` both set to the user, and one RoleBinding per requested Role selecting it, in the same transaction. No person is created for it.
2. Signing in with the token makes the user the request's user, and puts the token on the request context.
3. A check made as the request's own user is limited by the token: it must pass for both the owner and the token's own casbin subject, `token:<id>`. Checks made as another subject, e.g. a playbook during a run the request started, aren't limited, and neither are the owner's other sessions and tokens.

Each of a token's RoleBindings is compiled like any other binding, under its own principal, and `token:<id>` is grouped into it. The token API reloads the policy from the database after the token's transaction commits and before it returns, so the token works as soon as it's returned. Policy reloads MUST run one at a time, each holding one lock from reading the database to swapping in the new policy. _Why:_ casbin reads the database under a read lock and swaps under the write lock, so two reloads can read at once and the one that read before the commit can swap last, dropping the token. With reloads serialized, any reload that swaps after the token API's reload also read after it, so after the commit. A token created by the `auth token` command, in another process, is picked up by the reload its commit notifies, which serialization keeps from being overwritten the same way. Mission Control runs as one replica, so a lock in the process is enough.

Every token binding to a large Role adds a copy of its rules, e.g. roughly 80 to 160 policies for `admin`, and casbin walks every policy on each check. That's accepted at the expected number of tokens. Sharing one copy of a Role's rules between bindings without a constraint would remove the cost, and is left for later.

`token:<id>` is never the subject of a request, gets no implicit grants (the `everyone` role a subject joins on its first check), and is refused as the subject of an access review or access search. A stored casbin row for it is ignored when the policy loads.

_Why a casbin subject per token:_ its bindings are compiled the same way as every other binding, so a token's limit needs no evaluator of its own.

Since the owner's sessions and tokens share a subject, caches of per-subject results (RLS payloads, access summaries, PostgREST JWTs) are also keyed by the token.

### Authentication

Access tokens are accepted as:

```http
Authorization: Bearer <token>
```

or with basic auth:

```text
username: token
password: <token>
```

A token is validated by hashing it and looking up the hash, then checking its expiry. Valid tokens are cached until they expire, and a deleted token's id is kept on a deny list so a cached copy can't be used.

### Revocation

Access tokens are revocable by deleting their `access_tokens` row, which also deletes the RoleBindings that select them.

---

## Signed tokens

Signed tokens are short-lived JWTs minted for a specific Mission Control subsystem or protocol. They are created after another authentication step and are not durable, database-backed credentials.

They are different from long-lived access tokens, which are opaque, stored hashed in the `access_tokens` table, and explicitly revocable.

### Token types

| Token                          | Where generated                                                      |                                                        Lifetime | Audience / consumer                                          | Use case                                                                                                                                    |
| ------------------------------ | -------------------------------------------------------------------- | --------------------------------------------------------------: | ------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------- |
| Internal PostgREST/session JWT | After authentication                                                 |                                                      Cached 1h; | Mission Control/PostgREST/RLS                                | Injected after successful auth. Carries `role`, `id`, and RLS claims used by PostgREST and DB row-level security.                           |
| Basic login cookie JWT         | Basic-auth login                                                     | Browser session cookie; same JWT format as internal session JWT | Mission Control browser auth                                 | Stores the internal JWT in an HTTP-only cookie for basic-auth UI sessions.                                                                  |
| Plugin invocation JWT          | Authorizing a plugin operation                                       |                Default 5m; configurable with `--plugin-jwt-ttl` | Plugin host/runtime                                          | Short-lived proof that Mission Control authorized a plugin operation. Sent via `X-Flanksource-Plugin-Invocation` or gRPC metadata.          |
| Upstream tunnel JWT            | `upstream/tunnel`                                                     |                                                             1m | Mission Control agent                                       | Upstream signs each yamux HTTP stream request. Sent via `X-Flanksource-Upstream-JWT`; agents verify with the JWK embedded in v2 agent access tokens, requiring issuer `mission-control`, audience `mission-control-agent`, and subject `mission-control-upstream`. |
| Embedded OIDC access/ID JWT    | Embedded OIDC provider in `auth/oidc`                                |                                                              1h | MCP/native OAuth clients and Mission Control auth middleware | Standards-compliant OAuth/OIDC token for MCP/native clients. Mission Control validates it and converts it into normal request auth context. |

### Not counted here

The embedded OIDC flow also creates short-lived protocol artifacts such as authorization codes and transaction cookies. Those are intermediaries for completing OAuth login and are not Mission Control access credentials by themselves.

### Signing keys and crypto material

Signing keys and secrets are not tokens, but they are critical authentication material for signed tokens.

Mission Control owns or generates one JWT signing key pair plus OIDC protocol crypto material.

| Key / secret        | Where                                                                                                                | Default storage                                                                                                  | Used for                                                                                                              | Notes                                                                                                      |
| ------------------- | -------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| JWT signing keypair | `auth/signing`; flag `--signing-private-key`; `dutyAPI.DefaultConfig.Postgrest.JWTSecret` | Private key is loaded from `.mission-control-signing-key.pem` or `--signing-private-key`; if missing, an ephemeral in-memory key is generated | Signs basic login cookie JWTs, plugin invocation JWTs, embedded OIDC access/ID JWTs, and PostgREST/session JWTs when Mission Control runs embedded PostgREST | Embedded PostgREST receives the derived public JWK in `Postgrest.JWTSecret`, enforces `jwt-aud=mission-control-postgrest`, and verifies RS256 tokens with the public key. External PostgREST keeps the configured symmetric `postgrest-jwt-secret` / `PGRST_JWT_SECRET`. |
| OIDC crypto key     | The embedded OIDC provider                                                                                           | Ephemeral in-memory random 32-byte key generated on startup                                                       | Zitadel OIDC provider crypto key; also used as the HMAC key for OIDC transaction cookies                              | Not a JWT signing key. Existing opaque OIDC bearer tokens and transaction cookies are invalidated on restart. |

### Public verification material

| Material         | Where                                                       | Used for                    | Notes                                                                               |
| ---------------- | ----------------------------------------------------------- | --------------------------- | ----------------------------------------------------------------------------------- |
| OIDC public keys | `oidc_public_keys` table; derived from the shared JWT signing key | Verifies embedded OIDC JWTs | Public half of the shared signing key. Safe to expose through JWKS/key-set endpoints. |
| Clerk JWKS URL   | `--clerk-jwks-url`                                          | Verifies Clerk session JWTs | External verification material; Mission Control does not generate these keys.       |
