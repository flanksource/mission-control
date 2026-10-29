# ExternalIdentityProvider Specification

## 1. What an ExternalIdentityProvider is

An ExternalIdentityProvider lets an external application send its own users to Mission Control. The application signs a JWT for the user with its own issuer, and Mission Control accepts it as a bearer token.

A provider answers three questions, in this order:

| Question                              | Answered by                 | Result                                    |
| ------------------------------------- | --------------------------- | ----------------------------------------- |
| Is this token real, and is it for us? | Top-level fields (Section 3) | The token is accepted or rejected         |
| Who is this person?                   | `identity` (Section 5)      | A person record                           |
| Which teams are they in?              | `identity.teams` (Section 6) | Team memberships, limited to an allowlist |

What the person may do is never decided by the provider. It comes from RoleBindings that select the person or their teams (`rolebindings.md`, Sections 2.1, 2.2 and 2.4). RoleBindings never read claims.

_Why:_ claims are interpreted in one place, the provider, so whoever writes a RoleBinding doesn't need to know the shape of any application's tokens. Access follows the person and their teams, which Mission Control stores, so "who is in this team" and "what could this person do" have answers when no request is in flight.

```yaml
apiVersion: mission-control.flanksource.com/v1
kind: ExternalIdentityProvider
metadata:
  name: oipa
  namespace: mission-control
spec:
  issuer: https://auth.oipa.example.com
  audience: https://mission-control.example.com
  identity:
    username: claims.user.id
    name: claims.user.profile.display_name
    email: claims.user.profile.email
    teams:
      mapper: |
        has(claims.org.roles) ? claims.org.roles.map(r, "acme-" + r) : []
      allowed: [acme-operator, acme-viewer]
```

A request carrying a token from `https://auth.oipa.example.com`, for the audience `https://mission-control.example.com`, is authenticated as the person `oipa:u-4821`:

```http
GET /api/catalog/summary
Authorization: Bearer eyJhbGciOiJSUzI1NiIsImtpZCI6Im9pcGEtMSJ9...
```

where the token's claims are:

```json
{
  "iss": "https://auth.oipa.example.com",
  "aud": ["https://mission-control.example.com"],
  "sub": "session-7f3a",
  "iat": 1790000000,
  "exp": 1790000300,
  "user": {
    "id": "u-4821",
    "profile": {
      "display_name": "Alice Sharma",
      "email": "alice@acme.example.com"
    }
  },
  "org": {
    "id": "acme",
    "roles": ["operator", "admin"]
  }
}
```

The mapper returns `acme-operator` and `acme-admin`. Only `acme-operator` is allowed, so Alice joins `acme-operator` and nothing is done with `acme-admin`.

| Field            | Type            | Required | Default                                         | Meaning                                                                   |
| ---------------- | --------------- | -------- | ----------------------------------------------- | ------------------------------------------------------------------------- |
| `issuer`         | string          | Yes      |                                                 | Must exactly match the token's `iss` claim. Can't be changed (Section 8). |
| `audience`       | string          | Yes      |                                                 | Must be one of the token's `aud` claims. Usually the Mission Control URL. |
| `jwksURL`        | string          | No       | `jwks_uri` of the issuer's OpenID configuration | Where the issuer's signing keys are fetched from (Section 4).             |
| `algorithms`     | list of strings | No       | `RS256`, `ES256`                                | Signature algorithms accepted (Section 3.1).                              |
| `maxTokenAge`    | duration        | No       | `5m`                                            | Longest lifetime (`exp - iat`) a token may have.                          |
| `identity`       | object          | Yes      |                                                 | Who the person is and which teams they're in (Sections 5 and 6).          |
| `disabled`       | bool            | No       | `false`                                         | Rejects every token from this provider, without removing it.              |

## 2. Authentication

Mission Control looks at a bearer token's `iss` claim before verifying it:

- If no provider has that issuer, the token isn't for an ExternalIdentityProvider. Mission Control's other authenticators (its own OIDC, Kratos, Clerk, basic auth) try it as usual.
- If a provider has that issuer, the token MUST pass that provider's verification (Section 3). A token that fails is rejected with `401 Unauthorized` and a `WWW-Authenticate` header. It isn't passed on to other authenticators.

A provider whose `issuer` is one Mission Control's own authentication uses, i.e. its embedded OIDC issuer or the configured Clerk issuer, is `Ready=False` and claims no tokens: they're left to Mission Control's own authenticators, as if the provider didn't exist. _Why:_ it would capture the tokens of Mission Control's own users and sign them in as provider people instead.

A verified token authenticates only the request it's sent with. There's no session or cookie: the application sends a token on every request.

**Note:** Mission Control's normal OIDC login uses cookies to authenticate API requests, not JWT bearer tokens. Native JWT bearer authentication is out of scope for now. Of issuers that overlap, only overlap between providers (Section 7) and with Mission Control's own issuers (above) is handled.

## 3. Verification

A token is accepted only when all of these hold:

| Check     | Rule                                                                                    |
| --------- | --------------------------------------------------------------------------------------- |
| Provider  | The provider isn't `disabled`.                                                          |
| Signature | Signed with one of `algorithms`, by a key from the provider's signing keys (Section 4). |
| `iss`     | Exactly `issuer`.                                                                       |
| `aud`     | Contains `audience`. `aud` may be a string or a list.                                   |
| `exp`     | Present, and not in the past.                                                           |
| `iat`     | Present, and not in the future.                                                         |
| `nbf`     | If present, not in the future.                                                          |
| Lifetime  | `exp - iat` is at most `maxTokenAge`.                                                   |
| Username  | `identity.username` returns a non-empty string without `*` (Section 5).                 |

Time checks tolerate 30 seconds of clock skew.

`maxTokenAge` keeps tokens short-lived. Mission Control can't revoke a token, so a stolen token is only useful until it expires. An application that needs longer sessions issues new tokens, not longer ones.

Every token that passes these checks is trusted. There is no further condition on its claims, so the issuer MUST be dedicated to the application (Section 10).

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

Updating a provider keeps its cached keys unless `jwksURL` changes. `issuer` can't change (Section 8).

## 5. Identity

Each user of a provider is a person, created on their first request. `identity` says how the person is made from the token:

| Field      | Required | Returns | Used for                                                                                             |
| ---------- | -------- | ------- | ---------------------------------------------------------------------------------------------------- |
| `username` | Yes      | string  | Identifies the person. Their key is `<provider name>:<username>`, e.g. `oipa:u-4821`. A key identifies one person among those not retired (Section 8). |
| `name`     | No       | string  | The person's display name. Falls back to the username.                                               |
| `email`    | No       | string  | Shown on the person. Never used to identify or match anyone.                                         |
| `teams`    | No       | object  | The person's teams (Section 6). Without it, the provider adds the person to no team.                 |

`username`, `name` and `email` are CEL expressions. `claims` holds every claim of the verified token, nested values included, so an expression can read a nested claim or combine several:

```yaml
identity:
  username: claims.user.id
  name: |
    [claims.user.first_name,
     has(claims.user.middle_name) ? claims.user.middle_name : "",
     claims.user.last_name].filter(s, s != "").join(" ")
  email: has(claims.user.work_email) ? claims.user.work_email : claims.user.email
```

_Why CEL:_ a path can only read one claim, and applications split names, nest profiles and fall back between fields. CEL is already how the team mapper reads claims, so the provider has one language.

- **Each expression fails on its own.** `username` failing, or not returning a non-empty string, rejects the token with `401`. A username that's a number, e.g. a numeric user id, MUST be converted with `string()`. `name` or `email` failing, or not returning a string, keeps the value stored on the person, or leaves it unset on their first request. _Why:_ only the username is needed to know who the request is from. A missing optional claim, e.g. one user without a middle name, must not lock that user out or blank a name that was there.
- **A username MUST NOT contain `*`.** A token whose username does is rejected with `401`. _Why:_ `<provider>:*` selects every user of the provider (`rolebindings.md`, Section 2.1). A user named `*` couldn't be told apart from it, and patterns aren't supported, so a username containing `*` couldn't be selected on its own.
- **Compiled when the provider is applied.** An expression that doesn't compile makes the provider invalid (Section 9).
- **The username MUST be unique and stable** in the external application. It's the person's key, and RoleBindings name it (`rolebindings.md`, Sections 2.1 and 2.4). A username that changes for the same user makes a new person, without the old one's teams or bindings. An expression MAY build the username, e.g. from several claims, but whoever writes it is responsible for its stability.
- **Name and email are updated** from the token whenever their expressions return a different value.
- **These people are separate from Mission Control's own users.** An external user whose email is `alice@example.com` is not the Mission Control user `alice@example.com`, and gets none of her access. The email is shown only: Mission Control doesn't verify it, so trusting it would let the external application impersonate anyone. Nothing is ever sent to it, notifications included.
- **Shown as a provider person.** Wherever a person is shown, e.g. in approvals, audit and team members, a provider person MUST be shown with their key, `<provider>:<username>`. _Why:_ the application chooses their name and email, so it could make them look exactly like a Mission Control user.
- **One issuer per person.** A person belongs to the provider that created them, and only that provider's issuer can sign in as them. A provider's `issuer` can't change, and deleting a provider retires its people (Section 8), so a different issuer under the same name never signs in as an existing person. _Why:_ the person's memberships were given to a user of one application. Another issuer, whoever controls it, mustn't inherit the person or their memberships. RoleBindings that name the provider's users are a separate, deliberate choice (Section 8).

## 6. Teams

```yaml
identity:
  teams:
    mapper: |
      has(claims.org.roles) ? claims.org.roles.map(r, "acme-" + r) : []
    allowed: [acme-operator, acme-viewer]
```

| Field     | Required                 | Meaning                                                                             |
| --------- | ------------------------ | ----------------------------------------------------------------------------------- |
| `mapper`  | Yes                      | A CEL expression over `claims` that returns a list of team names.                   |
| `allowed` | Yes, at least one team   | The only teams this provider may add anyone to.                                     |

The person's teams from this provider become exactly the teams the mapper returns that are also in `allowed`:

- **Allowed teams only.** A returned team that isn't in `allowed` is skipped and logged. Nothing is written for it. _Why:_ the application controls its claims. Without a fixed list, a claim it sets, by mistake or on purpose, could put its users in any team, `admins` included. The list is part of the provider, which only Mission Control admins may write (`overview.md`, "Who may manage these resources"), so it can't be computed from claims. An `allowed` that includes the shipped `admins` team is reported in the provider's status as a warning, since it lets the application's claims decide who is a Mission Control admin.
- **Existing teams only.** A returned team that doesn't exist is skipped and logged. The provider never creates teams. An `allowed` entry naming a team that doesn't exist is reported in the provider's status, and the provider stays valid. _Why:_ a team is created and bound by an admin. Creating one from a claim would make an empty team that grants nothing, or one whose name an admin later binds without knowing who's in it.
- **Kept in sync, both ways.** The person is added to returned teams they aren't in yet, and removed from teams this provider added them to that it no longer returns. _Why:_ leaving a group in the application has to remove what it gave, or access only ever grows.
- **Only its own memberships.** The provider never removes a membership it didn't add, e.g. one an admin added or one from an invite. A person an admin already added to a returned team stays a direct member. An admin who adds a person to a team the provider added them to makes it a direct membership, which the provider then never removes. An admin who removes a membership the provider added removes it until the provider's next sync returns it again; to keep a provider user out of a team, the admin changes `allowed` or the application's claims.
- **When it runs.** On every request, before the request is checked. Memberships are written only when the result differs from what's stored, and the request is checked with the result. _Why:_ the result can change without the claims changing, e.g. when an allowed team is created or `allowed` changes. Running on every request leaves nothing to track about what changed.
- **A failing mapper refuses the request.** If the expression fails, or doesn't return a list of strings, the request is rejected with `401`, with a body saying that team mapping failed, and the error is logged. _Why the body:_ the token itself verified, so the application needs to tell this apart from a bad token, rather than issue new tokens that fail the same way. The person's stored memberships stay as they are. _Why:_ a mapper fails when the claims aren't what it expects, e.g. a group claim the application leaves out once the user has no groups left. Letting the request through on the memberships it last had would keep access the application has revoked, and miss teams a deny binding selects. Keeping what's stored lets the user carry on once the mapper or claims are fixed.
- **A failed write changes nothing.** If the mapper succeeds but its memberships can't be written, the request fails with `500 Internal Server Error` and the stored memberships stay as they were, like a failed key fetch (Section 4).
- **Removing a team from `allowed`**, or removing `identity.teams` entirely, removes every membership this provider added to the teams no longer allowed, without waiting for requests.
- **Without `teams`**, the provider adds no one to any team. Its users get only what RoleBindings grant them by name (`rolebindings.md`, Section 2.4), or teams an admin adds them to.

Memberships are stored, like every other team membership. The person's access is what their stored teams and bindings grant. Their token only changes it through the mapper.

- **Memberships are last observed.** A membership the application revokes is removed on the person's next request whose mapper succeeds. A failing mapper rejects the request and keeps what's stored. Until then, it counts everywhere a membership counts, including where no request is involved, e.g. team approvers, access reviews and the list of a team's members. A person who never returns keeps their memberships until the provider changes them (Section 8). _Why:_ Mission Control isn't told about changes in the application between requests, so what it last saw is the best it knows.
- **Concurrent tokens.** Two tokens for the same person with different claims, sent at the same time, each sync the person's teams. The memberships end up as the last sync left them. _Why:_ a person is one user with one set of teams. Acting in different contexts per token isn't supported (Section 10).

## 7. Uniqueness

- `issuer` MUST be unique across all providers, since it's how a token finds its provider.
- `name` MUST be unique across namespaces, since RoleBindings and person keys refer to a provider by name only.

Both depend on the other providers, so a provider that breaks either rule isn't rejected. Of the providers that share an `issuer` or a `name`, the one with the earliest `metadata.creationTimestamp` wins, and on a tie the one whose namespace sorts first. The others are `Ready=False` (Section 8). The winner isn't affected, and a restart doesn't change which one wins.

A provider that loses doesn't claim the issuer the winner holds: tokens from that issuer are verified by the winner. Tokens from an issuer only the losing provider has are rejected with `401`. A provider holds its `name` and `issuer` while it exists, whether valid, invalid or disabled. Only deleting it passes them on: the next oldest then takes effect without being re-applied. _Why:_ deleting a provider retires its people (Section 8), so a name only changes hands once no live person has its keys.

A provider that's wrong on its own, like the examples in Section 9, is rejected (`overview.md`, "Rejected or not in effect").

## 8. How changes take effect

- A provider takes effect as soon as it's valid. An invalid provider is `Ready=False` with the reason and its tokens are rejected with `401`, as if it were `disabled`, keeping its people's memberships. A provider whose `issuer` is one of Mission Control's own claims no tokens instead (Section 2). There is no previous version to fall back to; the provider is whatever was last written.
- `disabled: true` rejects its tokens with `401`, keeping everything else, including its people's memberships.
- `issuer` can't be changed. A provider for another issuer is a new provider. _Why:_ the provider's people were signed in by its issuer, and a new issuer mustn't inherit them (Section 5).
- Adding to `identity.teams` or changing its mapper applies on each person's next request (Section 6). Removing a team from `allowed`, or removing `identity.teams`, applies at once.
- Deleting a provider means its tokens no longer find a provider (Section 2), so they're left to the other authenticators, which reject them. Its people are retired: kept for audit, but never signed in as or selected again, and removed from every team, including teams an admin added them to. A provider created later under the same name, or one that wins the name once this one is gone (Section 7), creates new people for its users.
- RoleBindings that name a provider's users, e.g. `people: [oipa:u-4821]`, refer to the provider by name. They stay after the provider is deleted, and select the users of whichever provider later has that name. _Why:_ a binding is written for an application, which the provider name stands for. Whoever replaces the provider is a Mission Control admin, who is responsible for what the bindings to that name grant.

## 9. Validity

```yaml
# issuer over plain http
spec:
  issuer: http://auth.oipa.example.com
  audience: https://mission-control.example.com
  identity:
    username: claims.sub
```

```yaml
# Symmetric algorithm
spec:
  issuer: https://auth.oipa.example.com
  audience: https://mission-control.example.com
  algorithms: [HS256]
  identity:
    username: claims.sub
```

```yaml
# maxTokenAge not a positive duration
spec:
  issuer: https://auth.oipa.example.com
  audience: https://mission-control.example.com
  maxTokenAge: 0s
  identity:
    username: claims.sub
```

```yaml
# Missing audience
spec:
  issuer: https://auth.oipa.example.com
  identity:
    username: claims.sub
```

```yaml
# Missing identity.username
spec:
  issuer: https://auth.oipa.example.com
  audience: https://mission-control.example.com
  identity:
    name: claims.name
```

```yaml
# An identity expression that doesn't compile
spec:
  issuer: https://auth.oipa.example.com
  audience: https://mission-control.example.com
  identity:
    username: claims.user.
```

```yaml
# A team mapper without an allowlist
spec:
  issuer: https://auth.oipa.example.com
  audience: https://mission-control.example.com
  identity:
    username: claims.sub
    teams:
      mapper: claims.groups
```

## 10. Limitations

- Providers are held in memory by the Mission Control process that reconciled them, which assumes a single replica.
- Only JWT bearer tokens are supported: no opaque tokens, token introspection or userinfo lookups.
- Tokens can't be revoked before they expire. `disabled` or deleting the provider is the only way to cut off all of its users at once.
- Every token the issuer signs is trusted. Shared issuers, which sign tokens for many unrelated applications or organisations, e.g. Google, GitHub Actions or multi-tenant Microsoft Entra, aren't supported: there's no condition on claims to tell their users apart.
- Access belongs to the person, not the token. An application can't give the same user different access per request, e.g. by switching a tenant claim between tokens. Each switch would rewrite the person's teams (Section 6).
- A provider user can't be removed from a team the provider maps them into while their claims keep returning it. Change `allowed` or the claims instead (Section 6).
