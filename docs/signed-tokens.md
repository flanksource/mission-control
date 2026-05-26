# Signed tokens

Signed tokens are short-lived JWTs minted for a specific Mission Control subsystem or protocol. They are created after another authentication step and are not durable, database-backed credentials.

They are different from long-lived [access tokens](./access-tokens.md), which are opaque, stored hashed in the `access_tokens` table, and explicitly revocable.

## Token types

| Token                          | Where generated                                                      |                                                        Lifetime | Audience / consumer                                          | Use case                                                                                                                                    |
| ------------------------------ | -------------------------------------------------------------------- | --------------------------------------------------------------: | ------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------- |
| Internal PostgREST/session JWT | `auth.GetOrCreateJWTToken(...)` in `auth/tokens.go`                  |                    Cached 1h; currently no explicit `exp` claim | Mission Control/PostgREST/RLS                                | Injected after successful auth. Carries `role`, `id`, and RLS claims used by PostgREST and DB row-level security.                           |
| Basic login cookie JWT         | `auth.BasicLogin(...)` in `auth/basic.go`                            | Browser session cookie; same JWT format as internal session JWT | Mission Control browser auth                                 | Stores the internal JWT in an HTTP-only cookie for basic-auth UI sessions.                                                                  |
| Plugin invocation JWT          | `auth.MintPluginInvocationToken(...)` in `auth/plugin_invocation.go` |                Default 5m; configurable with `--plugin-jwt-ttl` | Plugin host/runtime                                          | Short-lived proof that Mission Control authorized a plugin operation. Sent via `X-Flanksource-Plugin-Invocation` or gRPC metadata.          |
| Embedded OIDC access/ID JWT    | Embedded OIDC provider in `auth/oidc`                                |                                                              1h | MCP/native OAuth clients and Mission Control auth middleware | Standards-compliant OAuth/OIDC token for MCP/native clients. Mission Control validates it and converts it into normal request auth context. |

## Not counted here

The embedded OIDC flow also creates short-lived protocol artifacts such as authorization codes and transaction cookies. Those are intermediaries for completing OAuth login and are not Mission Control access credentials by themselves.

Signing keys/secrets are also not tokens:

- PostgREST JWT secret
- Plugin JWT secret
- OIDC RSA signing key
- OIDC crypto key

## Relationship to access tokens

At a high level Mission Control has two credential families:

1. **Access tokens** — long-lived, opaque, DB-backed, revocable credentials.
2. **Signed tokens** — short-lived JWTs minted for a specific subsystem or protocol.

Access tokens can be used to authenticate a request. After authentication, Mission Control may mint an internal signed token for downstream use.
