# Token Bindings

A token binding is a RoleBinding whose subject is an access token. It isn't a kind of its own: it's stored, compiled and reported like any RoleBinding (`rolebindings.md`). What sets it apart comes from its subject: a token isn't an identity (`access-tokens.md`), so binding a Role to it limits the token instead of granting anything.

## 1. What it does

A token may do what its owner may do, and only what a rule of one of its bindings' Roles also matches (`limiting-permissions.md`, "Access tokens"). Every check made with a token is checked as its owner too, so a binding can only narrow what the owner can already do with that token. It adds nothing to the owner, to anyone else, or to the token beyond its owner.

## 2. Who may write it

A person MAY create the RoleBindings of their own tokens without any grant on RoleBindings. This is the only way a person writes a RoleBinding without one (`overview.md`, "Access").

- **Only when the token is created.** A token's bindings MUST be created with the token, one per Role it's created with, in the same transaction: the token and all of its bindings are created, or none of them is (`access-tokens.md`, Section 2). Nothing else MAY create them.
- **Never changed.** A token's bindings MUST NOT be changed, and none MAY be added or removed, while the token exists. To change what a token may do, its owner creates a new token. _Why:_ a token's limit is fixed when it's handed out, so whoever holds it can rely on it, and no endpoint can lift it later.
- **Not as a custom resource, nor through the RoleBinding API.** Neither has a field that selects a token, so neither can write a token binding. The RoleBinding API MUST refuse to change or delete one.
- **One token, the caller's own.** A token binding MUST select exactly one token, which the caller owns and created, and no other subject. It MUST NOT be written through a token, nor for a token of an agent or of another person.
- **Any Role.** A token MAY be bound to any Role, named by namespace and name, whether or not its owner may read it. Its status then reveals whether a Role of that name exists, but none of its rules. _Why:_ a Role's name is accepted as discoverable; its rules aren't.
- **Deleted with the token.** A token's bindings MUST be deleted in the same transaction as the token, however it's deleted: by its owner, or when it expires.

_Why no grant:_ writing RoleBindings is a high privilege because a binding grants: it gives a Role to people, teams and resources. A token binding grants nothing, since it can only narrow a token already capped at its owner. Requiring a grant on RoleBindings would keep anyone without that high privilege from creating a usable token, and protect nothing.

_Why not a custom resource:_ a binding from Git or the RoleBinding API has no caller to own a token, and is written by whoever may write RoleBindings. Letting it select tokens would let one person change another's credential, and widen a token its owner chose to narrow.

## 3. Where it's shown

- **With the token.** A person listing their tokens sees each token's bindings, without needing to read RoleBindings.
- **Not with other RoleBindings.** The Role Bindings page MUST NOT show token bindings, and the RoleBinding API MUST NOT list them unless asked to include them, e.g. `include=access-tokens`. Including them takes the same `read` on RoleBindings as listing any other binding. _Why:_ a token binding grants nothing, so among the bindings that decide who has access it's noise. Including them on request keeps them auditable.
- **Picking Roles.** When creating a token, a person picks from the Roles they may read, and MAY type any other as `namespace/name`.

## 4. How it's stored

| Field        | Value |
| ------------ | ----- |
| `namespace`  | `access-tokens` |
| `name`       | `token-<token id>.<role namespace>.<role name>` |
| `role`       | `<role namespace>/<role name>` |
| `subjects`   | `accessToken: <token id>`, and nothing else |
| `source`     | `AccessToken` |
| `created_by` | The token's owner |

- **By id.** The token is selected by its id, so a token deleted and created again with the same name isn't selected. _Why:_ the binding belongs to that one credential.
- **Found by its subject.** A token's bindings are those whose `subjects.accessToken` is its id. The name is a readable label only, and nothing relies on it.
- **In `access-tokens`, not the Role's namespace.** The shipped namespace holds only what Mission Control ships (`overview.md`, "Shipped Roles"), and a token has no namespace of its own.
- **Like any binding otherwise.** It's validated and reported as `rolebindings.md`, Section 4 says. A binding whose Role is missing or invalid is `Ready=False` and matches nothing, so the token can do nothing through it. A binding whose token no longer exists selects nothing.

_Why a RoleBinding:_ a token's limit is a Role bound to the token, which is what a RoleBinding is. Storing it as one keeps a single way to bind a Role, compiled, reported and narrowed the same way, instead of a second list of Roles on the token.

_Why no link table or column:_ a token's bindings are only ever written with it and deleted with it, in one transaction, so a lookup by subject is enough. A foreign key would only add a guarantee for tokens deleted outside Mission Control, whose leftover bindings select nothing.

## 5. Not covered yet

- **Bindings beyond the owner's access. TODO.** A token can be bound to a Role with rules its owner doesn't hold. Those rules do nothing today, but take effect for the token as soon as its owner gains matching grants. Nothing warns about this, at creation or later. A warning needs to tell whether a Role's rules are within the owner's grants for any resources, not only those that exist today, and that is left for a later design.
