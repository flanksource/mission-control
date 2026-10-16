# Token Bindings

A token binding is a RoleBinding whose subject is an access token. It isn't a kind of its own: it's stored, compiled and reported like any RoleBinding (`rolebindings.md`). What sets it apart comes from its subject: a token isn't an identity (`access-tokens.md`), so binding a Role to it limits the token instead of granting anything.

## 1. What it does

A token may do what its owner may do, and only what a rule of one of its bindings' Roles also matches (`limiting-permissions.md`, "Access tokens"). Every check made with a token is checked as its owner too, so a binding can only narrow what the owner can already do with that token. It adds nothing to the owner, to anyone else, or to the token beyond its owner.

## 2. Who may write it

A person may create, change and delete the RoleBindings of their own tokens without any grant on RoleBindings. This is the only way a person writes a RoleBinding without one (`overview.md`, "Access").

- **Only through the token API.** They're written when a token is created, and through the endpoint that changes a token's bindings. They MUST NOT be written as a custom resource or through the RoleBinding API, which have no field that selects a token.
- **One token, the caller's own.** A token binding selects exactly one token, which the caller owns and created, and no other subject. It MUST NOT be written through a token, or for a token of an agent or of another person.
- **Any Role.** A token may be bound to any Role, named by namespace and name, whether or not its owner may read it. Its status then reveals whether a Role of that name exists, but none of its rules. _Why:_ a Role's name is accepted as discoverable; its rules aren't.
- **Deleted with the token.** A token's bindings are deleted when it is.
- **Listed with the token.** A person listing their tokens sees each token's bindings, without needing to read RoleBindings.

_Why no grant:_ writing RoleBindings is a high privilege because a binding grants: it gives a Role to people, teams and resources. A token binding grants nothing, since it can only narrow a token already capped at its owner. Requiring a grant on RoleBindings would keep anyone without that high privilege from creating a usable token, and protect nothing.

_Why not a custom resource:_ a binding from Git or the RoleBinding API has no caller to own a token, and is written by whoever may write RoleBindings. Letting it select tokens would let one person change another's credential, and widen a token its owner chose to narrow.

## 3. How it's stored

| Field       | Value |
| ----------- | ----- |
| `namespace` | `access-tokens` |
| `name`      | `token-<token id>.<role namespace>.<role name>` |
| `role`      | `<role namespace>/<role name>` |
| `subjects`  | `accessTokens: [<token id>]`, and nothing else |
| `source`    | `AccessToken` |

- **By id.** The token is selected by its id, so a token deleted and created again with the same name isn't selected. _Why:_ the binding belongs to that one credential.
- **In `access-tokens`, not the Role's namespace.** The shipped namespace holds only what Mission Control ships (`overview.md`, "Shipped Roles"), and a token has no namespace of its own.
- **Read-only through the RoleBinding API.** It's listed like any other binding, but the RoleBinding API refuses to change or delete it.
- **Like any binding otherwise.** It's validated and reported as `rolebindings.md`, Section 4 says. A binding whose Role is missing or invalid is `Ready=False` and matches nothing, so the token can do nothing through it.

_Why a RoleBinding:_ a token's limit is a Role bound to the token, which is what a RoleBinding is. Storing it as one keeps a single way to bind a Role, compiled, reported and narrowed the same way, instead of a second list of Roles on the token.
