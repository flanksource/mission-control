# ExternalIdentityProvider Specification

## 1. What an ExternalIdentityProvider is

An ExternalIdentityProvider lets an external application send its own users to Mission Control. The application signs a JWT for the user with its own issuer, and Mission Control accepts it as a bearer token.

```yaml
apiVersion: mission-control.flanksource.com/v1
kind: ExternalIdentityProvider
metadata:
  name: oipa
  namespace: mission-control
spec:
  issuer: https://auth.oipa.example.com
  audience: https://mission-control.example.com
  claims:
    username: sub
    name: name
    email: email
```

A request carrying a token from `https://auth.oipa.example.com`, for the audience `https://mission-control.example.com`, is authenticated as the user in its `sub` claim:

```http
GET /api/catalog/summary
Authorization: Bearer eyJhbGciOiJSUzI1NiIsImtpZCI6Im9pcGEtMSJ9...
```

where the token's claims are:

```json
{
  "iss": "https://auth.oipa.example.com",
  "aud": "https://mission-control.example.com",
  "sub": "u-4821",
  "name": "Alice",
  "email": "alice@tenant-a.example.com",
  "groups": ["operators"],
  "tenant": "a",
  "iat": 1790000000,
  "exp": 1790000300
}
```

The provider only says which tokens are trusted. What the user may do comes from RoleBindings with `oidc` subjects (`rolebindings.md`, Section 2.4).

| Field            | Type            | Required | Default                          | Meaning                                                                          |
| ---------------- | --------------- | -------- | -------------------------------- | -------------------------------------------------------------------------------- |
| `issuer`         | string          | Yes      |                                  | Must exactly match the token's `iss` claim.                                      |
| `audience`       | string          | Yes      |                                  | Must be one of the token's `aud` claims. Usually the Mission Control URL.        |
| `jwksURL`        | string          | No       | `jwks_uri` of the issuer's OpenID configuration | Where the issuer's signing keys are fetched from (Section 4).      |
| `algorithms`     | list of strings | No       | `RS256`, `ES256`                 | Signature algorithms accepted (Section 3.1).                                     |
| `maxTokenAge`    | duration        | No       | `5m`                             | Longest lifetime (`exp - iat`) a token may have.                                 |
| `claims`         | object          | No       |                                  | Which claims describe the user (Section 5).                                      |
| `disabled`       | bool            | No       | `false`                          | Rejects every token from this provider, without removing it.                     |

## 2. Authentication

Mission Control looks at a bearer token's `iss` claim before verifying it:

- If no provider has that issuer, the token isn't for an ExternalIdentityProvider. Mission Control's other authenticators (its own OIDC, Kratos, Clerk, basic auth) try it as usual.
- If a provider has that issuer, the token MUST pass that provider's verification (Section 3). A token that fails is rejected with `401 Unauthorized` and a `WWW-Authenticate` header. It isn't passed on to other authenticators.

A verified token authenticates only the request it's sent with. There's no session or cookie: the application sends a token on every request.

**Note:** Mission Control's normal OIDC login uses cookies to authenticate API requests, not JWT bearer tokens. Native JWT bearer authentication and issuer-overlap handling are out of scope for now.

## 3. Verification

A token is accepted only when all of these hold:

| Check      | Rule                                                                                      |
| ---------- | ----------------------------------------------------------------------------------------- |
| Provider   | The provider isn't `disabled`.                                                            |
| Signature  | Signed with one of `algorithms`, by a key from the provider's signing keys (Section 4).   |
| `iss`      | Exactly `issuer`.                                                                         |
| `aud`      | Contains `audience`. `aud` may be a string or a list.                                     |
| `exp`      | Present, and not in the past.                                                             |
| `iat`      | Present, and not in the future.                                                           |
| `nbf`      | If present, not in the future.                                                            |
| Lifetime   | `exp - iat` is at most `maxTokenAge`.                                                     |
| Username   | The `claims.username` claim is present and a non-empty string (Section 5).               |

Time checks tolerate 30 seconds of clock skew.

`maxTokenAge` keeps tokens short-lived. Mission Control can't revoke a token, so a stolen token is only useful until it expires. An application that needs longer sessions issues new tokens, not longer ones.

### 3.1 Algorithms

Only asymmetric algorithms are accepted: `RS256`, `RS384`, `RS512`, `PS256`, `PS384`, `PS512`, `ES256`, `ES384`, `ES512`, `EdDSA`.

With a shared secret (`HS256`), anything able to verify a token could also mint one. `none` is never accepted.

## 4. Signing keys

The provider's signing keys are a JWKS, fetched from:

1. `jwksURL`, when set.
2. Otherwise, the `jwks_uri` of `<issuer>/.well-known/openid-configuration`.

Keys MUST be fetched over https, since a key swapped in transit lets anyone mint tokens:

- `issuer`, `jwksURL` and a discovered `jwks_uri` MUST use `https`. Plain `http` is only allowed for `localhost` and loopback addresses, for local development.
- Redirects are followed at most 5 times, and each MUST pass the same rule. A redirect from https to http is refused.

Keys are fetched on the first token from the provider, not when the provider is created, so a provider can be created before its issuer is reachable. After that they're cached and refreshed in the background, so a key the issuer rotates in is picked up without changing the provider.

If the keys can't be fetched, the request fails with `500 Internal Server Error`, and the next token retries the fetch.

Updating a provider keeps its cached keys unless `issuer` or `jwksURL` change.

## 5. Users

Each user of a provider is represented by a person record, created on their first request, for display and audit. It's never matched by `people` subjects (`rolebindings.md`, Section 2).

| Claim mapping      | Default | Used for                                                                                                  |
| ------------------ | ------- | --------------------------------------------------------------------------------------------------------- |
| `claims.username`  | `sub`   | Identifies the user. The person is keyed by `<provider name>:<username>`, e.g. `oipa:u-4821`.            |
| `claims.name`      |         | The person's display name. Falls back to the username.                                                    |
| `claims.email`     |         | Shown on the person. Never used to identify or match anyone.                                              |

- The username value MUST be unique and stable in the external application. The person is identified by the provider name and that value, regardless of which claim supplies it.
- Name and email are updated from the token whenever they change.
- **TODO:** the key reuses the provider name. A provider deleted and recreated under the same name with a different issuer lands its users on the old person records (`oipa:u-4821` from either issuer). Decide whether the key should include something that changes with the issuer, before person records exist in the wild.
- These people are separate from Mission Control's own users. An external user whose `email` claim is `alice@example.com` is not the Mission Control user `alice@example.com`, and gets none of her access: Mission Control doesn't verify the claim, so trusting it would let the external application impersonate anyone.

## 6. Access

An external user gets exactly what the RoleBindings matching their token grant, and nothing else:

- No built-in role, not even `viewer` or `everyone`. The default access described in `overview.md` doesn't apply to them.
- Permissions never apply to them, not even a Permission whose subject is their person record.
- A RoleBinding reaches them only through an `oidc` subject naming this provider. `people`, `teams` and `roles` subjects never match them. To select every user of the provider, use `match: "true"`.
- Every claim of the token is available to `match`, not only the ones in `claims`:

  ```yaml
  kind: RoleBinding
  metadata:
    name: tenant-a-operators
  spec:
    role: production-operator
    subjects:
      oidc:
        - provider: oipa
          match: "'operators' in claims.groups && claims.tenant == 'a'"
  ```

  With the token in Section 1, Alice gets `production-operator`.

- Bindings are matched on every request, so the user's access follows their latest token. If the next token has `"tenant": "b"`, Alice loses `tenant-a-operators` on that request.
- Their reads come only from `read` rules, and they list only what those rules allow (`roles.md`, Section 3.1). They have no built-in access that lists anything unfiltered.

## 7. Uniqueness

- `issuer` MUST be unique across all providers, since it's how a token finds its provider.
- `name` MUST be unique across namespaces, since RoleBindings and person keys refer to a provider by name only.

Both depend on the other providers, so a provider that breaks either rule isn't rejected. Of the providers that share an `issuer` or a `name`, the one with the earliest `metadata.creationTimestamp` wins, and on a tie the one whose namespace sorts first. The others are `Ready=False` (Section 8). The winner isn't affected, and a restart doesn't change which one wins.

A provider that loses doesn't claim the issuer the winner holds: tokens from that issuer are verified by the winner. Tokens from an issuer only the losing provider has are rejected with `401`. Once the winner is deleted or changed so they no longer conflict, the next oldest takes effect without being re-applied.

A provider that's wrong on its own, like the examples in Section 9, is rejected (`overview.md`, "Rejected or not in effect").

## 8. Changes

- A provider takes effect as soon as it's valid. An invalid provider is `Ready=False` with the reason and its tokens are rejected with `401`, as if it were `disabled`. There is no previous version to fall back to; the provider is whatever was last written.
- `disabled: true` rejects its tokens with `401`, keeping everything else.
- Deleting a provider means its tokens no longer find a provider (Section 2), so they're left to the other authenticators, which reject them. Its people remain, with no access.

## 9. Validity

```yaml
# issuer over plain http
spec:
  issuer: http://auth.oipa.example.com
  audience: https://mission-control.example.com
```

```yaml
# Symmetric algorithm
spec:
  issuer: https://auth.oipa.example.com
  audience: https://mission-control.example.com
  algorithms: [HS256]
```

```yaml
# maxTokenAge not a positive duration
spec:
  issuer: https://auth.oipa.example.com
  audience: https://mission-control.example.com
  maxTokenAge: 0s
```

```yaml
# Missing audience
spec:
  issuer: https://auth.oipa.example.com
```

## 10. Limitations

- Providers are held in memory by the Mission Control process that reconciled them, which assumes a single replica.
- Only JWT bearer tokens are supported: no opaque tokens, token introspection or userinfo lookups.
- Tokens can't be revoked before they expire. `disabled` or deleting the provider is the only way to cut off all of its users at once.
